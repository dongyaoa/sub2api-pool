package repository

import (
	"context"
	"database/sql"
	"time"
)

func (r *intelligenceMonitorRepository) IntelligenceExecutionBinding(ctx context.Context, accountID int64, at time.Time) (map[string]any, error) {
	// Keep the identity selected at the actual forwarding time, while recording
	// its latest display names when the artwork is saved. Archived identities can
	// still provide names; missing records fall back to the binding snapshot.
	rows, err := r.db.QueryContext(ctx, `SELECT b.target_id,COALESCE(t.name,b.target_name),b.supplier_id,COALESCE(s.name,b.supplier_name)
FROM upstream_account_bindings b
LEFT JOIN upstream_targets t ON t.id=b.target_id
LEFT JOIN upstream_suppliers s ON s.id=b.supplier_id
WHERE b.account_id=$1 AND b.valid_from<=$2
AND (b.valid_until IS NULL OR b.valid_until>$2) ORDER BY b.valid_from DESC,b.id DESC LIMIT 2`, accountID, at)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]any{"execution_binding_status": "unbound"}
	count := 0
	for rows.Next() {
		var id int64
		var name, supplierName string
		var supplierID sql.NullInt64
		if err = rows.Scan(&id, &name, &supplierID, &supplierName); err != nil {
			return nil, err
		}
		count++
		if count > 1 {
			return map[string]any{"execution_binding_status": "ambiguous"}, nil
		}
		out["execution_binding_status"] = "matched"
		out["execution_upstream_target_id"], out["execution_upstream_target_name"] = id, name
		if supplierID.Valid {
			out["execution_supplier_id"], out["execution_supplier_name"] = supplierID.Int64, supplierName
		}
	}
	return out, rows.Err()
}
