"""Check the inventory command against CLI and Actions SARIF layouts."""
import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


REPORT_SCRIPT = Path(__file__).resolve().parents[4] / 'src/tools/codeql-report.py'


def sarif(grouped=False, language='go'):
    rule_id = f'{language}/sql-injection'
    rule = {
        'id': rule_id,
        'defaultConfiguration': {'level': 'error'},
        'properties': {'security-severity': '8.8'},
    }
    driver = {'name': 'CodeQL'}
    tool = {'driver': driver}
    result = {
        'ruleId': rule_id,
        'message': {'text': 'Untrusted input reaches a query: café'},
        'locations': [{'physicalLocation': {
            'artifactLocation': {'uri': 'src/query.go'},
            'region': {'startLine': 12, 'startColumn': 5},
        }}],
    }
    if grouped:
        tool['extensions'] = [{'name': f'codeql/{language}-queries', 'rules': [rule]}]
        result['rule'] = {'id': rule_id, 'index': 0, 'toolComponent': {'index': 0}}
    else:
        driver['rules'] = [rule]
        result['ruleIndex'] = 0
    return {'version': '2.1.0', 'runs': [{'tool': tool, 'results': [result]}]}


class CodeQLReportTests(unittest.TestCase):
    def inventory(self, *reports):
        with tempfile.TemporaryDirectory(prefix='schakker-codeql-report-') as directory:
            directory = Path(directory)
            inputs = []
            for index, report in enumerate(reports):
                source = directory / f'{index}.sarif'
                source.write_text(json.dumps(report, ensure_ascii=False), encoding='utf-8')
                inputs.append(str(source))
            output = directory / 'review.json'
            completed = subprocess.run(
                [sys.executable, str(REPORT_SCRIPT), *inputs, '--output', str(output)],
                capture_output=True, text=True,
            )
            self.assertEqual(completed.returncode, 0, completed.stdout + completed.stderr)
            return json.loads(output.read_text(encoding='utf-8'))['findings']

    def test_actions_and_cli_keep_identical_findings_and_ids(self):
        cli = self.inventory(sarif())
        actions = self.inventory(sarif(grouped=True))
        self.assertEqual(actions, cli)
        self.assertEqual(len(actions), 1)
        finding = actions[0]
        self.assertEqual(finding['rule'], 'go/sql-injection')
        self.assertEqual(finding['level'], 'error')
        self.assertEqual(finding['security_severity'], '8.8')
        self.assertEqual((finding['file'], finding['line'], finding['column']),
                         ('src/query.go', 12, 5))
        self.assertEqual(finding['message'], 'Untrusted input reaches a query: café')
        self.assertEqual(finding['decision'], 'pending review')

    def test_driver_and_extension_findings_are_both_retained(self):
        report = sarif()
        extra = sarif(grouped=True, language='js')['runs'][0]
        report['runs'][0]['tool']['extensions'] = extra['tool']['extensions']
        report['runs'][0]['results'].extend(extra['results'])
        findings = self.inventory(report)
        self.assertEqual([item['rule'] for item in findings],
                         ['go/sql-injection', 'js/sql-injection'])
        self.assertTrue(all(item['security_severity'] == '8.8' for item in findings))

    def test_empty_scan_does_not_require_a_rules_array(self):
        report = {'version': '2.1.0', 'runs': [{'tool': {'driver': {'name': 'CodeQL'}}}]}
        self.assertEqual(self.inventory(report), [])

    def test_multiple_files_and_runs_keep_all_findings(self):
        first = sarif()
        second = sarif(grouped=True, language='js')
        second['runs'].extend(copy.deepcopy(first['runs']))
        findings = self.inventory(first, second)
        self.assertEqual(len(findings), 3)
        self.assertEqual([item['rule'] for item in findings],
                         ['go/sql-injection', 'js/sql-injection', 'go/sql-injection'])
        self.assertEqual(findings[0], findings[2])


if __name__ == '__main__':
    unittest.main()
