# -*- coding: utf-8 -*-
"""Tests for the collecting half of mod_wotctx, run with an ordinary Python 2.7:

    C:/Python27/python.exe -m unittest discover -s mod

Nothing here imports the game; client objects are replaced by fakes carrying
the attribute names the 2.4.0.1 client uses (IzeBerg/wot-src, branch EU).
"""
import json
import os
import shutil
import tempfile
import unittest

import mod_wotctx as m

NOW = 1790348400.0  # 2026-09-25T15:00:00Z


class FakeStats(object):
    credits = 1890865
    gold = 6347
    crystal = 12169
    freeXP = 251507
    unlocks = set()


class RenamedStats(object):
    """A client whose update renamed one attribute."""
    credits = 1
    gold = 2
    freeXP = 4


class CollectTest(unittest.TestCase):

    def test_header(self):
        h = m.header(NOW, '2.4.0.1', 512345678)
        self.assertEqual(h, {
            'schema': 1,
            'mod_version': m.MOD_VERSION,
            'captured_at': '2026-09-25T15:00:00Z',
            'game_version': '2.4.0.1',
            'account_id': 512345678,
        })

    def test_resources_map_client_names(self):
        self.assertEqual(m.collect_resources(FakeStats()), {
            'credits': 1890865, 'gold': 6347, 'bonds': 12169, 'free_xp': 251507})

    def test_a_renamed_attribute_is_a_missing_field_not_a_failure(self):
        r = m.collect_resources(RenamedStats())
        self.assertIsNone(r['bonds'])
        self.assertEqual(r['credits'], 1)

    def test_dump_path_is_beside_the_cache(self):
        self.assertEqual(m.dump_path({'LOCALAPPDATA': 'C:\\Users\\x\\AppData\\Local'}),
                         os.path.join('C:\\Users\\x\\AppData\\Local', 'wotctx', 'mod', 'garage.json'))


class WriteTest(unittest.TestCase):

    def setUp(self):
        self.dir = tempfile.mkdtemp()

    def tearDown(self):
        shutil.rmtree(self.dir)

    def test_write_creates_the_directory_and_replaces_the_file(self):
        path = os.path.join(self.dir, 'wotctx', 'mod', 'garage.json')
        m.write_dump(path, m.build_dump(NOW, '2.4.0.1', 1, FakeStats()))
        m.write_dump(path, m.build_dump(NOW + 60, '2.4.0.1', 1, FakeStats()))
        with open(path, 'rb') as f:
            dump = json.loads(f.read())
        self.assertEqual(dump['captured_at'], '2026-09-25T15:01:00Z')
        self.assertEqual(dump['resources']['bonds'], 12169)
        self.assertFalse(os.path.exists(path + '.tmp'))


class FakeScheduler(object):
    """BigWorld.callback, with time advanced by hand."""

    def __init__(self):
        self.queue = []

    def __call__(self, delay, fn):
        self.queue.append(fn)

    def run(self):
        queue, self.queue = self.queue, []
        for fn in queue:
            fn()


class SettlerTest(unittest.TestCase):

    def test_a_burst_runs_once(self):
        calls, sched = [], FakeScheduler()
        s = m.Settler(3.0, sched, lambda: calls.append(1))
        s.poke()
        s.poke()
        s.poke()
        sched.run()
        self.assertEqual(calls, [1])

    def test_the_sequence_that_lost_every_dump_in_the_client(self):
        # Lobby shown before the account has synced, then the sync completes
        # a few seconds later. Version 0.2.0 threw the second event away.
        synced, written, sched = [False], [], FakeScheduler()

        def flush():
            if synced[0]:
                written.append(1)

        s = m.Settler(3.0, sched, flush)
        s.poke()           # onAccountShowGUI
        sched.run()        # 3 s later: not synced, nothing written
        synced[0] = True
        s.poke()           # onSyncCompleted
        sched.run()
        self.assertEqual(written, [1])


class OnceLogTest(unittest.TestCase):

    def test_each_message_once(self):
        lines = []
        log = m.OnceLog(lines.append)
        log('skipped: account not synced yet')
        log('skipped: account not synced yet')
        log('skipped: not in the lobby')
        self.assertEqual(lines, ['skipped: account not synced yet', 'skipped: not in the lobby'])


class NoNetworkTest(unittest.TestCase):

    def test_the_mod_imports_no_network_module(self):
        with open(os.path.join(os.path.dirname(os.path.abspath(__file__)), 'mod_wotctx.py')) as f:
            source = f.read()
        for name in ('socket', 'urllib', 'urllib2', 'httplib', 'requests', 'BigWorld.fetchURL'):
            self.assertNotIn('import %s' % name, source)
            self.assertNotIn(name + '(', source)


# --- Fakes for a whole vehicle, named as the 2.4.0.1 client names them ------

class Obj(object):
    def __init__(self, **kw):
        self.__dict__.update(kw)


def item(cd, name, user):
    return Obj(intCD=cd, name=name, userName=user)


class FakeDossier(object):
    def __init__(self, records):
        self.records = records

    def getRecordValue(self, block, key):
        assert block == 'achievements'
        return self.records.get(key, 0)


class FakeWotPlus(object):
    def hasSubscription(self):
        return True


class BrokenCrewVehicle(object):
    intCD = 2
    name = 'x:broken'
    userName = 'Broken'
    level = 8
    xp = 5
    isElite = False
    isRented = False
    optDevices = Obj(installed=[])
    shells = Obj(installed=[])
    consumables = Obj(installed=[])
    battleBoosters = Obj(installed=[])

    @property
    def crew(self):
        raise AttributeError('crew renamed by an update')


def tvp():
    skill = Obj(name='commander_sixthSense', level=100)
    return Obj(
        intCD=60417, name='czech:Cz04_T50_51', userName='TVP T 50/51', level=10,
        xp=12345, isElite=True, isRented=False,
        optDevices=Obj(installed=[item(1, 'improvedVentilation_tier3', 'Improved Ventilation'), None, None]),
        shells=Obj(installed=[Obj(intCD=7, name='_105mm_AP', userName='AP', type='ARMOR_PIERCING', count=40)]),
        consumables=Obj(installed=[item(3, 'largeRepairkit', 'Large Repair Kit')]),
        battleBoosters=Obj(installed=[None]),
        crew=[(0, Obj(role='commander', skills=[skill], bonusSkills={'radioman': [Obj(name='radioman_finder'), None]})),
              (1, None)],
    )


class VehicleTest(unittest.TestCase):

    def test_premium_comes_from_the_client(self):
        stats = Obj(isPremium=True, activePremiumType='premium_plus', activePremiumExpiryTime=NOW)
        self.assertEqual(m.collect_premium(stats, FakeWotPlus()), {
            'premium': True, 'premium_type': 'premium_plus',
            'premium_expires_at': '2026-09-25T15:00:00Z', 'wot_plus': True})

    def test_marks_percent_is_the_stored_value_over_100(self):
        marks = m.collect_marks(FakeDossier({'marksOnGun': 2, 'damageRating': 8734, 'movingAvgDamage': 3120}))
        self.assertEqual(marks, {'marks': 2, 'percent': 87.34, 'moving_avg_damage': 3120})

    def test_a_whole_vehicle(self):
        errors = []
        v = m.collect_vehicle(tvp(), FakeDossier({'marksOnGun': 1, 'damageRating': 7000}), errors)
        self.assertEqual(errors, [])
        self.assertEqual(v['tank_id'], 60417)
        self.assertEqual(v['xp'], 12345)
        self.assertEqual(v['equipment'][0]['name'], 'improvedVentilation_tier3')
        self.assertEqual(v['equipment'][1:], [None, None])  # empty slots keep their places
        self.assertEqual(v['shells'][0]['count'], 40)
        self.assertEqual(v['shells'][0]['kind'], 'ARMOR_PIERCING')
        self.assertEqual(v['directives'], [None])
        self.assertEqual(v['crew'][0]['skills'], [{'name': 'commander_sixthSense', 'level': 100}])
        self.assertEqual(v['crew'][0]['bonus_skills'], {'radioman': ['radioman_finder']})
        self.assertEqual(v['crew'][1], {'slot': 1, 'role': None, 'skills': []})
        self.assertEqual(v['marks']['percent'], 70.0)

    def test_one_broken_part_costs_one_field(self):
        errors = []
        v = m.collect_vehicle(BrokenCrewVehicle(), None, errors)
        self.assertIsNone(v['crew'])
        self.assertEqual(v['xp'], 5)
        self.assertEqual(len(errors), 1)
        self.assertIn('vehicle 2 crew', errors[0])

    def test_build_dump_is_json_and_sorted(self):
        dossiers = {60417: FakeDossier({'marksOnGun': 3}), 2: None}
        dump = m.build_dump(NOW, '2.4.0.1', 1, FakeStats(), wot_plus=FakeWotPlus(),
                            vehicles=[tvp(), BrokenCrewVehicle()], dossier_for=dossiers.get)
        self.assertEqual([v['tank_id'] for v in dump['vehicles']], [2, 60417])
        self.assertEqual(dump['premium']['wot_plus'], True)
        self.assertEqual(len(dump['errors']), 1)
        json.loads(json.dumps(dump))  # everything in it serialises

    def test_unlocked_keeps_only_vehicles(self):
        # 2417 and 6257 are vehicles (type 1); 1058 (0x422) is type 2, a chassis.
        self.assertEqual(m.collect_unlocked(set([6257, 1058, 2417])), [2417, 6257])

    def test_the_dump_carries_unlocked_vehicles(self):
        stats = FakeStats()
        stats.unlocks = set([6257, 1058])
        dump = m.build_dump(NOW, '2.4.0.1', 1, stats)
        self.assertEqual(dump['unlocked'], [6257])

    def test_errors_are_capped(self):
        errors = []
        for i in range(m.MAX_ERRORS + 5):
            m._note(errors, str(i))
        self.assertEqual(len(errors), m.MAX_ERRORS)


if __name__ == '__main__':
    unittest.main()
