export type { User, Database, Group } from "../../../src/frontend/app/types";
import type { User, Database, Group } from "../../../src/frontend/app/types";
export type PageData = {
  Title: string;
  Active: string;
  Section: string;
  User: User;
  CSRF: string;
  Message: string;
  Now: string;
  Expires: string;
  Administrators?: User[] | null;
  Admin: {
    Users: User[] | null;
    Groups: Group[] | null;
    Databases: Database[] | null;
  };
  Groups:
    | (Group & {
        Members: User[] | null;
        Grants: { Database: Database; Permission: string }[] | null;
        Editor: string;
        Focus: boolean;
      })[]
    | null;
  DBMS: string[] | null;
};
export type Fields = Record<string, string | string[]>;
export type APIResult = Partial<PageData> & {
  location?: string;
  error?: string;
};
