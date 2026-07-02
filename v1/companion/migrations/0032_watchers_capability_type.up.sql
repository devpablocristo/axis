ALTER TABLE companion_watchers
    DROP CONSTRAINT IF EXISTS companion_watchers_watcher_type_check,
    ADD CONSTRAINT companion_watchers_watcher_type_check CHECK (watcher_type IN (
        'capability', 'stale_work_orders', 'unconfirmed_appointments',
        'low_stock', 'inactive_customers', 'revenue_drop'
    ));
