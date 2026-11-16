package dao

import (
	"context"
	"schakker_shared/observability"
)

func (d *Store) QueueMetrics(ctx context.Context) ([]observability.QueueSample, error) {
	rows, err := d.q.QueryContext(ctx, `SELECT j.operation,j.status,count(*),sum(GREATEST(j.attempt-1,0)),
 COALESCE(sum(p.committed_batches),0),COALESCE(sum(p.committed_rows),0),COALESCE(sum(p.reverted_batches),0),COALESCE(sum(p.reverted_rows),0)
 FROM migration_jobs j LEFT JOIN migration_batch_progress p ON p.job_id=j.id GROUP BY j.operation,j.status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []observability.QueueSample{}
	for rows.Next() {
		var sample observability.QueueSample
		if err = rows.Scan(&sample.Operation, &sample.Status, &sample.Jobs, &sample.Retries, &sample.Batches, &sample.Rows, &sample.RevertedBatches, &sample.RevertedRows); err != nil {
			return nil, err
		}
		result = append(result, sample)
	}
	return result, rows.Err()
}
