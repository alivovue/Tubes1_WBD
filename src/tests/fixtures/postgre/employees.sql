BEGIN;
CREATE TABLE public.employees (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    salary INTEGER
);
CREATE INDEX employees_salary_idx ON public.employees (salary);
INSERT INTO public.employees (name, salary)
VALUES ('Alice', 5000), ('Bob', 6000);
COMMIT;
