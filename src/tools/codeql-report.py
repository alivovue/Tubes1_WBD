#!/usr/bin/env python3
"""Create a reviewable inventory from genuine CodeQL SARIF, without suppressions."""
import argparse
import hashlib
import json
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument('sarif', nargs='+', type=Path)
parser.add_argument('--output', type=Path, required=True)
args = parser.parse_args()
findings = []
for source in args.sarif:
    data = json.loads(source.read_text(encoding='utf-8'))
    for run in data['runs']:
        # The CLI puts rules on the driver. CodeQL Actions groups them by
        # query pack under tool.extensions; results still carry their ruleId.
        components = [run['tool']['driver'], *run['tool'].get('extensions', [])]
        rules = {
            rule['id']: rule
            for component in components
            for rule in component.get('rules', [])
        }
        for result in run.get('results', []):
            location = result['locations'][0]['physicalLocation']
            rule = rules[result['ruleId']]
            finding = {
                'rule': result['ruleId'],
                'level': result.get('level', rule.get('defaultConfiguration', {}).get('level')),
                'security_severity': rule.get('properties', {}).get('security-severity'),
                'file': location['artifactLocation']['uri'],
                'line': location['region']['startLine'],
                'column': location['region'].get('startColumn'),
                'message': result['message']['text'],
                'potential_impact': '',
                'decision': 'pending review',
                'rationale': '',
                'fix_commit': None,
            }
            finding['id'] = hashlib.sha256(json.dumps({key: finding[key] for key in ['rule', 'file', 'line', 'column', 'message']}, sort_keys=True).encode()).hexdigest()[:16]
            findings.append(finding)
args.output.parent.mkdir(parents=True, exist_ok=True)
args.output.write_text(json.dumps({'findings': findings}, indent=2) + '\n', encoding='utf-8')
print(f'{len(findings)} findings inventoried in {args.output}')
