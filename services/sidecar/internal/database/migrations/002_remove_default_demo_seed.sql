-- v0.1 originally populated the repository-local development database by
-- default. Remove only those records whose stable IDs belong to that demo.
DELETE FROM tasks
WHERE id IN (
    '018f0000-0000-7000-8000-000000000101',
    '018f0000-0000-7000-8000-000000000102'
);

DELETE FROM projects
WHERE id = '018f0000-0000-7000-8000-000000000002';

DELETE FROM clients
WHERE id = '018f0000-0000-7000-8000-000000000001';
