"""Build/verify a standard portable demo package; never start the Mock service."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil

ROOT = Path(__file__).resolve().parents[1]
VERSION = 'v1.5.2'


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def sources():
    files = {
        'README.md': 'docs/FORMAT_DEMO_DELIVERY_README.md',
        'start.cmd': 'start_format_demo.cmd',
        f'hda_mocker_4_{VERSION}.exe': f'hda_mocker_4_{VERSION}.exe',
        'skills/hda-data-import/SKILL.md': 'skills/hda-data-import/SKILL.md',
        'tools/reformat_dataset.py': 'tools/reformat_dataset.py',
        'tools/build_format_demo_preset.py': 'tools/build_format_demo_preset.py',
    }
    for name in ('config.yaml', 'summary.md', 'manifest.json', 'hda/format_demo.parquet', 'da/format_demo.parquet'):
        files[f'presets/format_demo/{name}'] = f'presets/format_demo/{name}'
    for name in ('HDA_DA_FORMAT_AND_PLAYBACK_GUIDE.md', 'HDA_DA_VERIFICATION.md',
                 'HDA_MOCKER_4_DATASET_SUBMISSION_RULE.md', 'HDA_MOCKER_4_RELEASE_1_5_0.md',
                 'HDA_MOCKER_4_RELEASE_1_5_1.md', 'HDA_MOCKER_4_RELEASE_1_5_2.md'):
        files[f'docs/{name}'] = f'docs/{name}'
    return files


def verify(output):
    metadata = json.loads((output / 'package_manifest.json').read_text(encoding='utf-8'))
    if metadata['version'] != VERSION:
        raise ValueError('Package version mismatch')
    expected = set(sources())
    if set(metadata['files']) != expected:
        raise ValueError('Package file inventory mismatch')
    actual = {p.relative_to(output).as_posix() for p in output.rglob('*') if p.is_file()}
    if actual != expected | {'package_manifest.json'}:
        raise ValueError('Missing or unexpected files (runtime/logs must not be packaged)')
    for name, info in metadata['files'].items():
        path = output / name
        if not path.is_file() or path.stat().st_size != info['bytes'] or sha(path) != info['sha256']:
            raise ValueError(f'Package integrity mismatch: {name}')
    configs = list((output / 'presets').glob('*/config.yaml'))
    if configs != [output / 'presets/format_demo/config.yaml'] or (output / 'config.yaml').exists():
        raise ValueError('Expected exactly one config in the standard preset directory')
    data = json.loads((output / 'presets/format_demo/manifest.json').read_text(encoding='utf-8'))
    for info in data['files']:
        if sha(output / 'presets/format_demo' / info['file']) != info['sha256']:
            raise ValueError('Parquet differs from dataset manifest')
    links = 0
    for document in output.rglob('*.md'):
        for link in re.findall(r'\]\(([^)]+)\)', document.read_text(encoding='utf-8')):
            if '://' in link or link.startswith('#'):
                continue
            target = (document.parent / link.split('#')[0]).resolve()
            if not target.is_relative_to(output) or not target.is_file():
                raise ValueError(f'Broken/outside link in {document}: {link}')
            links += 1
    print(f'PASS: {len(expected)} payload files, all hashes and {links} documentation links; no runtime or service start.')


def build(output):
    if output.exists():
        raise ValueError('Refusing to overwrite an existing output; choose a new --output or use --verify')
    for source in sources().values():
        if not (ROOT / source).is_file():
            raise ValueError(f'Missing {source}; build the versioned EXE and generate demo data first')
    output.mkdir(parents=True)
    for target, source in sources().items():
        destination = output / target
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(ROOT / source, destination)
        # Historical repository audits are deliberately not part of this tiny
        # teaching package. Mark those references as source-repository-only,
        # instead of shipping unrelated site data or leaving broken hyperlinks.
        if target in ('docs/HDA_MOCKER_4_RELEASE_1_5_0.md', 'docs/HDA_MOCKER_4_DATASET_SUBMISSION_RULE.md'):
            text = destination.read_text(encoding='utf-8')
            text = re.sub(r'\[([^\]]+)\]\(\.\./presets/FORMAT_MIGRATION_1_5_0\.md\)',
                          r'\1（源仓库 `presets/FORMAT_MIGRATION_1_5_0.md`；本包不附其他现场preset的迁移审计）', text)
            destination.write_text(text, encoding='utf-8')
    metadata = {'version': VERSION, 'files': {name: {'bytes': (output / name).stat().st_size, 'sha256': sha(output / name)} for name in sorted(sources())}}
    (output / 'package_manifest.json').write_text(json.dumps(metadata, ensure_ascii=False, indent=2), encoding='utf-8')
    verify(output)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=ROOT / 'build' / f'format_demo_{VERSION}')
    parser.add_argument('--verify', action='store_true')
    args = parser.parse_args()
    output = args.output.resolve()
    if output == ROOT or output in ROOT.parents:
        raise SystemExit('Output must not replace the source tree or one of its parents')
    (verify if args.verify else build)(output)
