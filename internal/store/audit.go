package store

import (
	"context"
	"encoding/json"
)

const (
	ActorUser     = "user"
	ActorCamera   = "camera"
	ActorSystem   = "system"
	ActorProvider = "provider"
)

type AuditEntry struct {
	ActorUserID *int64
	ActorKind   string
	Action      string
	Entity      string
	EntityID    string
	OldValue    any
	NewValue    any
	RequestID   string
}

func AppendAudit(ctx context.Context, q Querier, entry AuditEntry) error {
	oldValue, err := auditJSON(entry.OldValue)
	if err != nil {
		return err
	}
	newValue, err := auditJSON(entry.NewValue)
	if err != nil {
		return err
	}
	var requestID any
	if entry.RequestID != "" {
		requestID = entry.RequestID
	}
	_, err = q.Exec(ctx,
		`INSERT INTO "ya-audit_log"
		 (actor_user_id, actor_kind, action, entity, entity_id, old_value, new_value, request_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.ActorUserID, entry.ActorKind, entry.Action, entry.Entity, entry.EntityID,
		oldValue, newValue, requestID,
	)
	return err
}

func auditJSON(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}
