import http from 'k6/http';
import { sleep } from 'k6';
import { fixture } from './config.js';
import { currentTags, installSession, parameters, record } from './helpers.js';

export function apiTraffic(data) {
  installSession(data);
  const tags = currentTags('api');
  const database = fixture.database_ids[__ITER % fixture.database_ids.length];
  record(http.get(`${fixture.base_url}/api/databases`, parameters(data, 'databases', tags)), tags);
  record(http.get(`${fixture.base_url}/api/migration-jobs?database_id=${database}`, parameters(data, 'job_list', tags)), tags);
  sleep(0.1);
}
