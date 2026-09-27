# -*- coding: utf-8 -*-
"""wotctx client mod: a read-only dump of the garage, for wotctx to read.

The World of Tanks API has no per-vehicle XP, no mark percentages, no loadouts
and a wrong premium flag; the client knows all of them. This mod writes what the
client knows to one JSON file on this machine, and wotctx reads it as the source
`mod:garage` (docs/spec.md section 3.6, docs/plan.md phase 4).

What it does not do, by design: it makes no network request, reads nothing in
battle, handles no input and changes no UI. It writes only in the lobby, when
the client has finished syncing the account.

The file has two halves. The top half turns client objects into plain data and
imports nothing from the game, so it runs under an ordinary Python 2.7 for the
tests in test_mod_wotctx.py. The bottom half is the adapter that the game loads.

Python 2.7, because that is what the client embeds.
"""
import json
import os
import time

MOD_VERSION = '0.3.0'
SCHEMA = 1

# The client syncs after every battle, purchase and crew change, often several
# times in a burst. A write waits this long after the first event of a burst,
# so it catches the burst's end state and writes once.
SETTLE_SECONDS = 3.0


# --- Collecting: plain data out of client objects -------------------------

def dump_path(environ=None):
    """Where wotctx looks for the dump: beside its cache, not in the game
    folder, which Aslain's modpack installer manages."""
    environ = os.environ if environ is None else environ
    base = environ.get('LOCALAPPDATA') or environ.get('XDG_DATA_HOME') or \
        os.path.join(os.path.expanduser('~'), '.local', 'share')
    return os.path.join(base, 'wotctx', 'mod', 'garage.json')


def utc_stamp(now):
    """RFC 3339, UTC, whole seconds - what wotctx parses everywhere else."""
    return time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime(now))


def header(now, game_version, account_id):
    return {
        'schema': SCHEMA,
        'mod_version': MOD_VERSION,
        'captured_at': utc_stamp(now),
        'game_version': game_version,
        'account_id': account_id,
    }


def collect_resources(stats):
    """Money and free XP. Bonds are `crystal` in the client."""
    return {
        'credits': _read(stats, 'credits'),
        'gold': _read(stats, 'gold'),
        'bonds': _read(stats, 'crystal'),
        'free_xp': _read(stats, 'freeXP'),
    }


def collect_premium(stats, wot_plus):
    """The client's own premium state; the API's is wrong for this account
    (docs/spec.md section 3.1). wot_plus is IWotPlusController, or None."""
    expires = _read(stats, 'activePremiumExpiryTime')
    return {
        'premium': _read(stats, 'isPremium'),
        'premium_type': _read(stats, 'activePremiumType'),
        'premium_expires_at': utc_stamp(expires) if expires else None,
        'wot_plus': _call(wot_plus, 'hasSubscription'),
    }


def collect_item(item):
    """One fitted item - equipment, consumable, directive - or None for an
    empty slot. `name` is the client's technical name, stable across
    languages; `user_name` is what the garage shows."""
    if item is None:
        return None
    return {
        'id': _read(item, 'intCD'),
        'name': _read(item, 'name'),
        'user_name': _read(item, 'userName'),
    }


def collect_shell(shell):
    entry = collect_item(shell)
    if entry is not None:
        entry['kind'] = _read(shell, 'type')
        entry['count'] = _read(shell, 'count')
    return entry


def collect_marks(dossier):
    """Mark count and progress from the vehicle's dossier. damageRating is
    stored ×100: 8734 is the 87.34 % the garage shows."""
    if dossier is None:
        return None
    rating = dossier.getRecordValue('achievements', 'damageRating')
    return {
        'marks': dossier.getRecordValue('achievements', 'marksOnGun'),
        'percent': rating / 100.0 if rating else 0.0,
        'moving_avg_damage': dossier.getRecordValue('achievements', 'movingAvgDamage'),
    }


def collect_tankman(slot, tankman):
    if tankman is None:
        return {'slot': slot, 'role': None, 'skills': []}
    skills = []
    for skill in _read(tankman, 'skills') or []:
        skills.append({'name': _read(skill, 'name'), 'level': _read(skill, 'level')})
    bonus = {}
    for role, role_skills in (_read(tankman, 'bonusSkills') or {}).items():
        names = [_read(s, 'name') for s in role_skills if s is not None]
        if names:
            bonus[role] = names
    return {'slot': slot, 'role': _read(tankman, 'role'), 'skills': skills, 'bonus_skills': bonus}


def collect_vehicle(vehicle, dossier, errors):
    """Everything the client knows about one owned vehicle. Each part is
    collected on its own, so a part a game update broke is missing from the
    dump - and named in errors - while the rest still arrives."""
    cd = _read(vehicle, 'intCD')
    v = {
        'tank_id': cd,
        'name': _read(vehicle, 'name'),
        'user_name': _read(vehicle, 'userName'),
        'tier': _read(vehicle, 'level'),
        'xp': _read(vehicle, 'xp'),
        'elite': _read(vehicle, 'isElite'),
        'rented': _read(vehicle, 'isRented'),
    }
    parts = (
        ('marks', lambda: collect_marks(dossier)),
        ('equipment', lambda: [collect_item(i) for i in vehicle.optDevices.installed]),
        ('shells', lambda: [collect_shell(s) for s in vehicle.shells.installed]),
        ('consumables', lambda: [collect_item(i) for i in vehicle.consumables.installed]),
        ('directives', lambda: [collect_item(i) for i in vehicle.battleBoosters.installed]),
        ('crew', lambda: [collect_tankman(slot, t) for slot, t in vehicle.crew]),
    )
    for key, collect in parts:
        try:
            v[key] = collect()
        except Exception as e:
            v[key] = None
            _note(errors, 'vehicle %s %s: %r' % (cd, key, e))
    return v


VEHICLE_TYPE_ID = 1  # items.ITEM_TYPES['vehicle']: the low four bits of a compact descriptor


def collect_unlocked(unlocks):
    """The researched vehicles, from the client's set of every unlocked item
    (modules included). A vehicle's compact descriptor - the API's tank_id -
    carries type 1 in its low four bits."""
    return sorted(cd for cd in (unlocks or ()) if cd & 15 == VEHICLE_TYPE_ID)


def build_dump(now, game_version, account_id, stats, wot_plus=None, vehicles=(), dossier_for=None):
    """The whole dump. vehicles are the owned Vehicle items; dossier_for maps a
    tank_id to its dossier."""
    errors = []
    dump = header(now, game_version, account_id)
    dump['resources'] = collect_resources(stats)
    dump['premium'] = collect_premium(stats, wot_plus)
    try:
        dump['unlocked'] = collect_unlocked(stats.unlocks)
    except Exception as e:
        _note(errors, 'unlocked: %r' % (e,))
    dump['vehicles'] = []
    for vehicle in vehicles:
        try:
            dossier = dossier_for(_read(vehicle, 'intCD')) if dossier_for else None
        except Exception as e:
            dossier = None
            _note(errors, 'vehicle %s dossier: %r' % (_read(vehicle, 'intCD'), e))
        dump['vehicles'].append(collect_vehicle(vehicle, dossier, errors))
    dump['vehicles'].sort(key=lambda v: v['tank_id'])
    dump['errors'] = errors
    return dump


MAX_ERRORS = 20


def _note(errors, text):
    """Failures travel in the dump itself, capped, so a broken field can be
    diagnosed from wotctx without reading the client's log."""
    if len(errors) < MAX_ERRORS:
        errors.append(text)


def _call(obj, name, default=None):
    try:
        return getattr(obj, name)()
    except Exception:
        return default


def _read(obj, name, default=None):
    """One attribute, or default when a game update has renamed it. A dump
    with a field missing is still useful; a dump that is never written is not."""
    try:
        return getattr(obj, name)
    except Exception:
        return default


# --- Writing ----------------------------------------------------------------

def write_dump(path, dump):
    """Write atomically: a reader never sees half a file."""
    directory = os.path.dirname(path)
    if not os.path.isdir(directory):
        os.makedirs(directory)
    tmp = path + '.tmp'
    with open(tmp, 'wb') as f:
        f.write(json.dumps(dump, sort_keys=True, indent=1))
    _replace(tmp, path)


def _replace(src, dst):
    # Python 2's os.rename will not overwrite on Windows. MoveFileExW with
    # MOVEFILE_REPLACE_EXISTING does, in one step; without ctypes, fall back to
    # remove-then-rename, which leaves a moment with no file rather than a
    # corrupt one.
    try:
        import ctypes
        if ctypes.windll.kernel32.MoveFileExW(unicode(src), unicode(dst), 1):
            return
    except Exception:
        pass
    if os.path.exists(dst):
        os.remove(dst)
    os.rename(src, dst)


class Settler(object):
    """Runs fn once, `delay` seconds after the first poke of a burst; pokes
    while it waits change nothing.

    It replaces a throttle that let one call through per ten seconds - and that,
    in the first version in the client, spent its turn on the lobby's first
    event, when the account was not yet synced, then dropped the sync event
    that followed. Nothing is spent here: a run that finds the data not ready
    writes nothing, and the next sync event pokes again.

    schedule(delay, fn) is BigWorld.callback in the client."""

    def __init__(self, delay, schedule, fn):
        self._delay = delay
        self._schedule = schedule
        self._fn = fn
        self._waiting = False

    def poke(self):
        if not self._waiting:
            self._waiting = True
            self._schedule(self._delay, self._fire)

    def _fire(self):
        self._waiting = False
        self._fn()


class OnceLog(object):
    """Logs each distinct message once, so a condition that repeats on every
    sync is visible in the log without flooding it."""

    def __init__(self, log):
        self._log = log
        self._seen = set()

    def __call__(self, message):
        if message not in self._seen:
            self._seen.add(message)
            self._log(message)


# --- The adapter: everything below runs only inside the client --------------

def _log(*parts):
    try:
        from debug_utils import LOG_NOTE
        LOG_NOTE('[wotctx]', *parts)
    except Exception:
        pass


_log_once = OnceLog(_log)


def _schedule(delay, fn):
    import BigWorld
    BigWorld.callback(delay, fn)


def _in_lobby():
    """True only for the garage: in battle the player is an Avatar, and the
    mod stays out of battle entirely."""
    import BigWorld
    from Account import PlayerAccount
    return isinstance(BigWorld.player(), PlayerAccount)


def _items_cache():
    from helpers import dependency
    from skeletons.gui.shared import IItemsCache
    return dependency.instance(IItemsCache)


def _dump_now():
    """Write the dump, or return why not."""
    if not _in_lobby():
        return 'not in the lobby'
    cache = _items_cache()
    if not cache.isSynced():
        return 'account not synced yet'
    import BigWorld
    from helpers import dependency, getShortClientVersion
    from gui.shared.utils.requesters import REQ_CRITERIA
    try:
        from skeletons.gui.game_control import IWotPlusController
        wot_plus = dependency.instance(IWotPlusController)
    except Exception:
        wot_plus = None
    version = getShortClientVersion().strip().lstrip('v.')
    items = cache.items
    vehicles = items.getVehicles(REQ_CRITERIA.INVENTORY).values()
    dump = build_dump(time.time(), version, BigWorld.player().databaseID, items.stats,
                      wot_plus=wot_plus, vehicles=vehicles, dossier_for=items.getVehicleDossier)
    write_dump(dump_path(), dump)
    _log('wrote', len(dump['vehicles']), 'vehicles,', len(dump['errors']), 'errors')
    return None


def _flush():
    try:
        reason = _dump_now()
        if reason:
            _log_once('skipped: ' + reason)
    except Exception as e:
        # One line, never a traceback into the player's session.
        _log_once('dump failed: %r' % (e,))


_settler = Settler(SETTLE_SECONDS, _schedule, _flush)


def _on_sync_completed(*args):
    try:
        _settler.poke()
    except Exception as e:
        _log_once('scheduling failed: %r' % (e,))


def _on_account_show_gui(*args):
    _on_sync_completed()


def init():
    try:
        from PlayerEvents import g_playerEvents
        g_playerEvents.onAccountShowGUI += _on_account_show_gui
        _items_cache().onSyncCompleted += _on_sync_completed
        _log('loaded', MOD_VERSION, '->', dump_path())
    except Exception as e:
        _log('init failed:', repr(e))


def fini():
    try:
        from PlayerEvents import g_playerEvents
        g_playerEvents.onAccountShowGUI -= _on_account_show_gui
        _items_cache().onSyncCompleted -= _on_sync_completed
    except Exception:
        pass
