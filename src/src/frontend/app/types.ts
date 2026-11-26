export type User = {
  ID: number;
  Username: string;
  DisplayName: string;
  Active: boolean;
};
export type Database = {
  ID: number;
  Name: string;
  DBMS: string;
  DatabaseName: string;
  Host: string;
  Port: number;
  SSLMode: string;
  Active: boolean;
  Permission: string;
};
export type Group = { ID: number; Name: string; Description: string };
export type Column = {
  Name: string;
  DataType: string;
  Nullable: boolean;
  Default?: string | null;
  Identity?: string;
  GeneratedExpression?: string | null;
};
export type Constraint = {
  Name: string;
  Type: string;
  Definition?: string;
  Columns?: string[];
  ReferencedSchema?: string;
  ReferencedTable?: string;
  ReferencedColumns?: string[];
  UpdateRule?: string;
  DeleteRule?: string;
};
export type Index = {
  Name: string;
  Method: string;
  Unique: boolean;
  Definition?: string;
  Columns?: { Name: string }[];
};
export type TableDetail = {
  Table: { Name: string; Kind: string };
  Columns: Column[] | null;
  Constraints: Constraint[] | null;
  Indexes: Index[] | null;
};
export type Rows = {
  Columns: { Name: string; DataType: string }[] | null;
  Rows: unknown[][] | null;
  HasMore: boolean;
  Duration: number;
};
export type RecentExecution = Pick<
  MigrationExecution,
  "id" | "database_id" | "title" | "actor" | "operation" | "committed_at" | "version"
> & { database_name: string };
export type Draft = {
  ID: number;
  Title: string;
  SQL: string;
  UpdatedAt: string;
};
export type Operation = {
  Kind: string;
  Schema: string;
  Table: string;
  Column?: Column;
  Index?: Index;
  Definition?: { Columns: Column[]; Constraints: Constraint[] };
  Values?: Record<string, unknown>;
  Where?: Record<string, unknown>;
};
export type PlanNode = {
  Operation: string;
  Relation: string;
  Index: string;
  JoinType: string;
  EstimatedRows: number | null;
  StartupCost: number | null;
  TotalCost: number | null;
  FilteredPercent?: number | null;
  Children: PlanNode[] | null;
};
export type StatementPlan = {
  Statement: string;
  DBMS: string;
  Available: boolean;
  UnavailableReason?: "unsupported_statement" | "explain_unavailable";
  Nodes: PlanNode[] | null;
  JSON: unknown;
  Message: string;
};
export type Review = {
  SQL: string;
  BaseVersion: number;
  Mode: string;
  Transactional: boolean;
  Warnings: string[] | null;
  Plans: StatementPlan[] | null;
  SchemaDiffAvailable: boolean;
  SchemaDiffMessage: string;
  SchemaDiff: {
    Destructive: boolean;
    Changes:
      | { Kind: string; Schema: string; Table: string; Name: string }[]
      | null;
    DataChanges:
      | {
          Kind: string;
          Schema: string;
          Table: string;
          Values: unknown;
          Where: unknown;
        }[]
      | null;
  } | null;
};
export type DiffCell = {
  Line: number;
  Text: string;
  Kind: string;
  Marker: string;
};
export type PageData = {
  Title: string;
  Active: string;
  Section: string;
  User: User;
  CSRF: string;
  Message: string;
  Now: string;
  Expires: string;
  Databases: Database[] | null;
  Database: Database;
  CanEdit: boolean;
  EditableCount: number;
  Recent: RecentExecution[] | null;
  Schemas: { Name: string }[] | null;
  Tables: { Name: string }[] | null;
  Schema: string;
  Table: string;
  Sort: string;
  Direction: string;
  Detail: TableDetail | null;
  Data: Rows | null;
  Offset: number;
  Previous: number;
  Next: number;
  HasMore: boolean;
  Drafts: Draft[] | null;
  DraftID: number;
  DraftUpdated: string;
  Description: string;
  SQL: string;
  OperationsJSON: string;
  Review: Review | null;
  SchemaDiff: {
    Added: number;
    Removed: number;
    Rows: { Before: DiffCell; After: DiffCell }[];
  } | null;
  ViewerResult: Rows | null;
  ViewerSpec: { Limit: number; Offset: number };
  ScrollTarget: string;
};
export type Fields = Record<string, string | string[]>;
export type APIResult = Partial<PageData> & {
  location?: string;
  error?: string;
};

// M2 resource contracts. Kept separate from the existing M1 page/analysis DTOs.
export type MigrationAction =
  | "up"
  | "up_batch"
  | "rollback"
  | "forward"
  | "cancel_batch";
export type DefinitionStatus = "draft" | "submitted" | "executed";
export type ReviewStatus =
  | "pending_review"
  | "approved"
  | "rejected"
  | "invalidated";
export type JobStatus =
  | "pending"
  | "running"
  | "succeeded"
  | "failed"
  | "partial"
  | "uncertain"
  | "reverting"
  | "reverted"
  | "partially_reverted";
export type BatchConfig = {
  template: "update_by_key";
  schema: string;
  table: string;
  key_column: string;
  value_column: string;
  delta: number;
  batch_size: number;
  expected_rows?: number;
};
export type DefinitionInput = {
  database_id: number;
  title: string;
  up_sql: string;
  down_sql: string;
  irreversible: boolean;
  irreversibility_reason: string;
  batch: BatchConfig | null;
  expected_revision_id?: number;
};
export type MigrationDefinition = DefinitionInput & {
  id: number;
  revision_id: number;
  expected_generation: number;
  revision: number;
  status: DefinitionStatus;
  editable: boolean;
  definition_hash: string;
  created_by: string;
  updated_at: string;
};
export type RequiredApproval = {
  group_id: number;
  group_name: string;
  permission: "view_only" | "view_edit";
  leader_user_id: number | null;
  leader_name: string | null;
  decision: "pending" | "approved" | "rejected";
  decided_by?: string;
  decided_at?: string;
  reason?: string;
  can_decide: boolean;
};
export type MigrationReview = {
  id: number;
  database_id: number;
  definition_id: number;
  revision_id: number;
  expected_generation: number;
  revision: number;
  title: string;
  action: MigrationAction;
  target_execution_id: string | null;
  target_job_id: string | null;
  status: ReviewStatus;
  requested_by: string;
  requested_at: string;
  definition_hash: string;
  authority_revision: string;
  up_sql: string;
  down_sql: string;
  irreversible: boolean;
  irreversibility_reason: string;
  batch: BatchConfig | null;
  required_groups: RequiredApproval[];
  reason?: string;
  can_enqueue: boolean;
  can_resubmit: boolean;
  job_id?: string | null;
};
export type MigrationJob = {
  job_id: string;
  database_id: number;
  definition_id: number;
  revision_id: number;
  expected_generation: number;
  review_id: number;
  title: string;
  operation: MigrationAction;
  status: JobStatus;
  requested_by: string;
  created_at: string;
  started_at: string | null;
  completed_at: string | null;
  attempt: number;
  max_attempts: number;
  next_retry_at: string | null;
  committed_batches: number;
  committed_rows: number;
  reverted_batches: number;
  reverted_rows: number;
  total_rows: number | null;
  last_cursor: string | null;
  retryable: boolean;
  can_cancel: boolean;
  blocked_reason: string | null;
  error: string | null;
};
export type MigrationExecution = {
  id: string;
  database_id: number;
  definition_id: number;
  revision_id: number;
  expected_generation: number;
  revision: number;
  version: number;
  title: string;
  operation: MigrationAction;
  sql: string;
  actor: string;
  committed_at: string;
  duration_ms: number | null;
  job_id: string;
  review_id: number;
  target_execution_id: string | null;
  irreversible: boolean;
  irreversibility_reason: string;
  can_rollback: boolean;
  can_forward: boolean;
  action_blocked_reason?: string;
};
export type ResourceResponse = { CSRF?: string; error?: string };
export type DefinitionsResponse = ResourceResponse & {
  definitions: MigrationDefinition[];
};
export type DefinitionResponse = ResourceResponse & {
  definition: MigrationDefinition;
};
export type ReviewsResponse = ResourceResponse & { reviews: MigrationReview[] };
export type ReviewResponse = ResourceResponse & { review: MigrationReview };
export type JobsResponse = ResourceResponse & { jobs: MigrationJob[] };
export type JobResponse = ResourceResponse & { job: MigrationJob };
export type EnqueueResponse = ResourceResponse & {
  job_id: string;
  status: JobStatus;
  review_id: number;
};
export type HistoryResponse = ResourceResponse & {
  executions: MigrationExecution[];
  next_cursor: string | null;
};
