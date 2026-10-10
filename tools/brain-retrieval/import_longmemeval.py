#!/usr/bin/env python3
"""Offline, stdlib-only LongMemEval S converter. Gold never enters documents."""
import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import shutil

TYPES = ('single-session-user', 'single-session-assistant', 'single-session-preference',
         'temporal-reasoning', 'knowledge-update', 'multi-session')
VERSION = 'loom-retrieval-v1'


def encoded(value):
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(',', ':')) + '\n').encode()


def select(rows):
    if len({r['question_id'] for r in rows}) != len(rows):
        raise ValueError('duplicate question_id')
    chosen = []
    def take(bucket, count):
        candidates = sorted((r for r in rows if bucket(r) and r not in chosen),
                            key=lambda r: hashlib.sha256((VERSION + r['question_id']).encode()).digest())
        if len(candidates) < count:
            raise ValueError('insufficient selection bucket')
        chosen.extend(candidates[:count])
    for kind in TYPES:
        take(lambda r: r['question_type'] == kind and not r['question_id'].endswith('_abs'), 3)
    take(lambda r: r['question_id'].endswith('_abs'), 3)
    take(lambda r: r['question_type'] == 'knowledge-update' and not r['question_id'].endswith('_abs'), 3)
    return [r['question_id'] for r in chosen]


def convert(row):
    ids, dates, sessions = row['haystack_session_ids'], row['haystack_dates'], row['haystack_sessions']
    if not (len(ids) == len(dates) == len(sessions)) or len(set(ids)) != len(ids):
        raise ValueError('invalid full history/session mapping')
    case = {'id': row['question_id'], 'type': row['question_type'], 'query': row['question'],
            'query_time': row['question_date'], 'documents': [], 'sources': ['conversations']}
    gold = {'spans': [], 'sessions': [], 'temporal_review': 'pending' if row['question_type'] == 'knowledge-update' else 'not-applicable'}
    abstention = row['question_id'].endswith('_abs')
    for sid, date, turns in zip(ids, dates, sessions):
        # Source paths are opaque ordinals; raw session IDs stay in the sidecar.
        path = f"discussion/lme{len(case['documents']):04d}/session.md"
        text = f"# Discussion\n\nDate: {date}\n\n"
        for index, turn in enumerate(turns):
            content = turn['content']
            if not isinstance(content, str) or not content.strip():
                raise ValueError('invalid/empty turn')
            text += f"## Turn {index + 1} ({turn['role']})\n\n"
            start = len(text.encode())
            text += content + '\n\n'
            if turn.get('has_answer') and not abstention:
                gold['spans'].append({'source': 'conversations', 'path': path, 'text': content,
                                      'session': sid, 'turn': index, 'start': start,
                                      'end': start + len(content.encode())})
        if len(text.encode()) > 1 << 20:
            raise ValueError('history exceeds Brain file limit; no trimming allowed')
        case['documents'].append({'source': 'conversations', 'path': path, 'text': text})
        if sid in row['answer_session_ids'] and not abstention:
            gold['sessions'].append({'source': 'conversations', 'path': path, 'session': sid})
    if not abstention and (not gold['spans'] or not gold['sessions']):
        raise ValueError('missing gold evidence')
    if any(s['session'] not in row['answer_session_ids'] for s in gold['spans']):
        raise ValueError('turn/session gold mismatch')
    return case, gold


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', type=Path, required=True)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--output-dir', type=Path, required=True)
    parser.add_argument('--select-once', action='store_true', help='write fixed IDs/hashes to a NEW cache manifest for review')
    args = parser.parse_args()
    manifest = json.loads(args.manifest.read_text())
    raw = args.input.read_bytes()
    if hashlib.sha256(raw).hexdigest() != manifest['upstream']['sha256']:
        raise ValueError('input SHA does not match manifest')
    rows = json.loads(raw)
    by_id = {r['question_id']: r for r in rows}
    if len(by_id) != len(rows):
        raise ValueError('duplicate IDs')
    ids = manifest['selected_ids']
    if args.select_once:
        if ids or '.project-local' not in args.output_dir.resolve().parts:
            parser.error('select-once requires an empty selection and gitignored output directory')
        ids = select(rows)
        manifest['selected_ids'] = ids
    if len(ids) != 24 or len(set(ids)) != 24:
        raise ValueError('manifest needs 24 fixed reviewed IDs; use --select-once in cache for initial review')
    cases, evidence, hashes = [], {}, {}
    for case_id in ids:
        case, gold = convert(by_id[case_id])
        cases.append(case)
        evidence[case_id] = gold
        hashes[case_id] = hashlib.sha256(encoded(case) + encoded(gold)).hexdigest()
    counts = dict(sorted(Counter('abstention' if r['id'].endswith('_abs') else r['type'] for r in cases).items()))
    if not args.select_once and (hashes != manifest['converted_sha256'] or counts != manifest['category_counts']):
        raise ValueError('converted hashes/category counts differ from reviewed manifest')
    manifest['category_counts'], manifest['converted_sha256'] = counts, hashes
    manifest['selection_version'], manifest['converter_revision'] = VERSION, '1'
    manifest['natural_subset_status'] = '24 fixed cache IDs; history and temporal annotations require explicit review.'
    args.output_dir.mkdir(parents=True, exist_ok=True)
    (args.output_dir / 'cases.jsonl').write_bytes(b''.join(encoded(c) for c in cases))
    synthetic_dir = Path(__file__).resolve().parents[2] / 'internal/loom/brain/testdata/retrieval'
    evidence.update(json.loads((synthetic_dir / 'evidence.json').read_text()))
    shutil.copyfile(synthetic_dir / 'temporal.jsonl', args.output_dir / 'temporal.jsonl')
    (args.output_dir / 'evidence.json').write_bytes(encoded(evidence))
    manifest['files_sha256'] = {name: hashlib.sha256((args.output_dir / name).read_bytes()).hexdigest()
                                for name in ('cases.jsonl', 'evidence.json', 'temporal.jsonl')}
    (args.output_dir / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(f"Converted {len(cases)} full histories; temporal labels pending review; no model calls")


if __name__ == '__main__':
    main()
