export type User = {
  ID: number;
  Username: string;
  DisplayName: string;
  IsAdmin: boolean;
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
export type Execution = {
  ID: number;
  DatabaseID: number;
  DatabaseName?: string;
  Description: string;
  SQL: string;
  Username: string;
  StartedAt: string;
  Status: string;
  Mode: string;
  CompletedStatements: number;
  TotalStatements: number;
  AppliedVersion?: number;
  AttemptedVersion: number;
  ErrorMessage: string;
};
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
  Recent: Execution[] | null;
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
  History: Execution[] | null;
  Execution: Execution | null;
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
