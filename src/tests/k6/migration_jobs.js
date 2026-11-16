import http from 'k6/http';
import { sleep } from 'k6';
import { fixture } from './config.js';
import { currentTags, fixtureContention, installSession, jobFailures, jobsAccepted, parameters, pollJob, record } from './helpers.js';

export function migrationTraffic(data, mode) {
  installSession(data);
  const tags = currentTags(mode);
  const target = mode === 'same_database' ? 0 : 1 + (__VU % 2);
  // Fresh approvals must bind the current head. Fixture leases serialize review
  // preparation per target; production policy still validates every operation.
  const ready = http.post(`${fixture.base_url}/fixture/next?target=${target}`, '', {
    headers: { Authorization: `Bearer ${fixture.control_token}` },
    tags: { name: 'fixture_prepare', ...tags }, timeout: '30s',
    responseCallback: http.expectedStatuses(200, 409),
  });
  if (ready.status === 409) { fixtureContention.add(1, tags); sleep(0.2); return; }
  if (ready.status !== 200) { jobFailures.add(true, tags); sleep(0.5); return; }
  const review = ready.json();
  const params = parameters(data, 'enqueue_approved', tags);
  params.headers['Idempotency-Key'] = `k6-review-${review.review_id}`;
  // Actual simultaneous same-database delivery: both requests must reserve the
  // same durable job, and only one successful execution is counted.
  const url = `${fixture.base_url}/api/migration-reviews/${review.review_id}/enqueue`;
  const responses = http.batch([{ method: 'POST', url, body: '{}', params }, { method: 'POST', url, body: '{}', params }]);
  for (const response of responses) record(response, tags);
  if (responses.some((response) => response.status !== 202)) { jobFailures.add(true, tags); return; }
  const id = responses[0].json().job_id;
  if (responses[1].json().job_id !== id) { jobFailures.add(true, tags); return; }
  jobsAccepted.add(1, tags);
  pollJob(id, data, tags);
}
