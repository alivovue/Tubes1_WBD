CREATE TABLE IF NOT EXISTS employees (
    id INTEGER NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    salary INTEGER,
    INDEX employees_salary_idx (salary)
) ENGINE=InnoDB;

INSERT IGNORE INTO employees (id, name, salary)
VALUES (1, 'Alice', 5000), (2, 'Bob', 6000);
