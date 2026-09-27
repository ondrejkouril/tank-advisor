# -*- coding: utf-8 -*-
"""Build the wotctx client mod: compile mod_wotctx.py for the client's Python
2.7 and pack it as a .wotmod.

    C:/Python27/python.exe mod/build.py <out-dir>

The package follows every mod installed in the client: an uncompressed zip
(ZIP_STORED - the client does not read compressed packages) holding meta.xml
and res/scripts/client/gui/mods/mod_wotctx.pyc.
"""
import os
import py_compile
import shutil
import sys
import tempfile
import zipfile

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import mod_wotctx  # noqa: E402  (for MOD_VERSION, so there is one place to bump it)

MOD_ID = 'ondrejkouril.wotctx'
SCRIPT_DIR = 'res/scripts/client/gui/mods/'

META = """<root>
  <!-- Technical MOD ID -->
  <id>%s</id>
  <!-- Package version -->
  <version>%s</version>
  <!-- Human readable name -->
  <name>wotctx garage dump</name>
  <!-- Human readable description -->
  <description>Read-only: writes this account's garage (vehicle XP, marks, loadouts, crew, resources) to a local file for the wotctx tool. No network, nothing in battle.</description>
</root>
"""


def build(out_dir):
    if sys.version_info[:2] != (2, 7):
        raise SystemExit('build.py needs Python 2.7, the client\'s version; got %d.%d' % sys.version_info[:2])
    version = mod_wotctx.MOD_VERSION
    tmp = tempfile.mkdtemp()
    try:
        pyc = os.path.join(tmp, 'mod_wotctx.pyc')
        # dfile is the name tracebacks show. It is set to the in-game path so
        # that this machine's paths are not baked into the package.
        py_compile.compile(os.path.join(HERE, 'mod_wotctx.py'), cfile=pyc,
                           dfile='scripts/client/gui/mods/mod_wotctx.py', doraise=True)

        if not os.path.isdir(out_dir):
            os.makedirs(out_dir)
        package = os.path.join(out_dir, '%s_%s.wotmod' % (MOD_ID, version))
        with zipfile.ZipFile(package, 'w', zipfile.ZIP_STORED) as z:
            z.writestr('meta.xml', META % (MOD_ID, version))
            # Directory entries, as the installed packages carry them.
            for d in ('res/', 'res/scripts/', 'res/scripts/client/', 'res/scripts/client/gui/', SCRIPT_DIR):
                z.writestr(zipfile.ZipInfo(d), '')
            z.write(pyc, SCRIPT_DIR + 'mod_wotctx.pyc')
        print(package)
    finally:
        shutil.rmtree(tmp)


if __name__ == '__main__':
    build(sys.argv[1] if len(sys.argv) > 1 else 'dist')
