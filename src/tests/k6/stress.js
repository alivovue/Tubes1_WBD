import { apiTraffic } from './api.js';
import { migrationTraffic } from './migration_jobs.js';
import { phases, stageSeconds, stages, thresholds } from './config.js';
import { login } from './helpers.js';

export const options = {
  scenarios: {
    api: { executor: 'ramping-vus', exec: 'api', startVUs: 2, stages: stages([2, 6, 12, 2]), gracefulRampDown: '15s', gracefulStop: '30s' },
    same_database: { executor: 'ramping-vus', exec: 'sameDatabase', startVUs: 1, stages: stages([1, 2, 4, 1]), gracefulRampDown: '15s', gracefulStop: '30s' },
    different_databases: { executor: 'ramping-vus', exec: 'differentDatabases', startVUs: 2, stages: stages([2, 4, 8, 2]), gracefulRampDown: '15s', gracefulStop: '30s' },
  },
  thresholds,
  summaryTrendStats: ['avg', 'min', 'med', 'p(50)', 'p(95)', 'max'],
  systemTags: ['method', 'status', 'name', 'scenario', 'group', 'check', 'expected_response'],
};
export function setup() { return login(); }
export function api(data) { apiTraffic(data); }
export function sameDatabase(data) { migrationTraffic(data, 'same_database'); }
export function differentDatabases(data) { migrationTraffic(data, 'different_databases'); }
export function handleSummary(data) {
  const proof = {
    state: data.state,
    metrics: data.metrics,
    schakker: { phases, stage_seconds: stageSeconds, normal_vus: 5, peak_target_vus: 24, success_definition: 'terminal committed job, not HTTP 202', approval_fixture: 'real current-generation approvals, isolated PostgreSQL targets' },
  };
  const destination = __ENV.SCHAKKER_K6_SUMMARY || './summary.json';
  return { [destination]: JSON.stringify(proof, null, 2), stdout: `Schakker k6 report: ${destination}\n${data.metrics.jobs_completed?.values.count || 0} jobs completed; thresholds recorded in report.\n` };
}
