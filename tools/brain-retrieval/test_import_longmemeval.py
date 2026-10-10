"""Free deterministic importer checks; never download the upstream dataset."""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

MODULE = Path(__file__).with_name('import_longmemeval.py')
spec = importlib.util.spec_from_file_location('converter', MODULE)
converter = importlib.util.module_from_spec(spec)
spec.loader.exec_module(converter)


class ImportTests(unittest.TestCase):
    def rows(self):
        rows = []
        for kind in converter.TYPES:
            for index in range(6):
                rows.append(dict(question_id=f'{kind}-{index}', question_type=kind,
                                 question='Which deployment region?', question_date='2026-01-01',
                                 answer='ORACLE_ONLY_SECRET', haystack_session_ids=['distractor', 'support'],
                                 haystack_dates=['2024-01-01', '2025-01-01'],
                                 haystack_sessions=[[dict(role='user', content='Unrelated filler.')],
                                                   [dict(role='user', content='The deployment region is cobalt.', has_answer=True)]],
                                 answer_session_ids=['support']))
        for index in range(3):
            row = dict(rows[index], question_id=f'abstention-{index}_abs')
            rows.append(row)
        return rows

    def test_selection_conversion_and_hash_guard(self):
        rows = self.rows()
        ids = converter.select(rows)
        self.assertEqual(len(ids), 24)
        self.assertEqual(ids, converter.select(list(reversed(rows))))
        case, gold = converter.convert(rows[0])
        self.assertEqual(len(case['documents']), 2)  # retain every distractor
        self.assertEqual(len(gold['spans']), 1)
        span = gold['spans'][0]
        raw = case['documents'][1]['text'].encode()
        self.assertEqual(raw[span['start']:span['end']].decode(), span['text'])
        indexed = json.dumps(case)
        for oracle in ('ORACLE_ONLY_SECRET', 'has_answer', 'answer_session_ids', 'support'):
            self.assertNotIn(oracle, indexed)
        self.assertEqual(converter.convert(rows[-1])[1]['spans'], [])
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / '.project-local'
            root.mkdir()
            data = root / 'input.json'
            data.write_text(json.dumps(rows))
            manifest = root / 'manifest.json'
            manifest.write_text(json.dumps(dict(upstream=dict(sha256=hashlib.sha256(data.read_bytes()).hexdigest()),
                                                selected_ids=[], converted_sha256={}, category_counts={})))
            out = root / 'converted'
            cmd = ['python3', str(MODULE), '--input', str(data), '--manifest', str(manifest), '--output-dir', str(out)]
            subprocess.run(cmd + ['--select-once'], check=True, capture_output=True)
            fixed = json.loads((out / 'manifest.json').read_text())
            self.assertEqual(fixed['category_counts']['knowledge-update'], 6)
            self.assertEqual(fixed['category_counts']['abstention'], 3)
            self.assertEqual(fixed['selected_ids'], ids)
            cmd[cmd.index(str(manifest))] = str(out / 'manifest.json')
            subprocess.run(cmd, check=True, capture_output=True)
            data.write_text('[]')
            rejected = subprocess.run(cmd, capture_output=True, text=True)
            self.assertNotEqual(rejected.returncode, 0)
            self.assertIn('input SHA', rejected.stderr)

    def test_insufficient_bucket_and_duplicate_ids(self):
        with self.assertRaises(ValueError):
            converter.select(self.rows()[:2])
        with self.assertRaises(ValueError):
            converter.select(self.rows() + self.rows()[:1])


if __name__ == '__main__':
    unittest.main()
