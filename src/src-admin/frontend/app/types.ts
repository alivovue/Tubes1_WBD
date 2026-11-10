export type User = { ID: number; Username: string; DisplayName: string; Active: boolean; };
export type Group = { ID: number; Name: string; Description: string; };
export type Database = { ID:number; DBMS:string; Name:string; Host:string; Port:number; DatabaseName:string; SSLMode:string; Active:boolean; Permission:string; };
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
