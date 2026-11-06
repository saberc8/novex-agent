-- Read-only integrity checks; expected violations = 0 for every statement.

SELECT current_database(),pg_database_size(current_database()) AS size_bytes;

SELECT 'order_line_amount_mismatch' AS check_name,count(*) AS violations FROM order_items WHERE line_total<>quantity*unit_price;

SELECT 'order_total_mismatch' AS check_name,count(*) AS violations FROM orders o JOIN (SELECT tenant_id,order_id,sum(line_total) amount FROM order_items GROUP BY tenant_id,order_id)i USING(tenant_id,order_id) WHERE o.total_amount<>i.amount;

SELECT 'movement_order_line_mismatch' AS check_name,count(*) AS violations FROM stock_movements m JOIN order_items i USING(tenant_id,order_id,line_no) WHERE m.sku<>i.sku OR m.warehouse_id<>i.warehouse_id OR m.quantity<>i.quantity;

SELECT 'inventory_ledger_mismatch' AS check_name,count(*) AS violations FROM inventory i FULL JOIN (SELECT tenant_id,warehouse_id,sku,sum(CASE WHEN movement_type='inbound' THEN quantity ELSE -quantity END) balance FROM stock_movements GROUP BY tenant_id,warehouse_id,sku)m USING(tenant_id,warehouse_id,sku) WHERE i.stock IS DISTINCT FROM COALESCE(m.balance,0);

SELECT 'reserved_order_mismatch' AS check_name,count(*) AS violations FROM inventory i LEFT JOIN (SELECT oi.tenant_id,oi.warehouse_id,oi.sku,sum(oi.quantity) qty FROM order_items oi JOIN orders o USING(tenant_id,order_id) WHERE o.status='allocated' GROUP BY oi.tenant_id,oi.warehouse_id,oi.sku)r USING(tenant_id,warehouse_id,sku) WHERE i.reserved<>COALESCE(r.qty,0);

SELECT 'invalid_inventory' AS check_name,count(*) AS violations FROM inventory WHERE stock<0 OR reserved<0 OR reserved>stock;

SELECT 'unvalidated_foreign_keys' AS check_name,count(*) AS violations FROM pg_constraint WHERE contype='f' AND NOT convalidated;
