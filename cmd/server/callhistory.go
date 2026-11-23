package main

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Histórico de chamadas persistente: cada chamada encerrada (Broker.OnEnded) vira
// uma linha em call_history no banco principal, e as mais recentes voltam para a
// memória ao subir o servidor — o histórico do painel sobrevive a reinícios.

const callHistoryLoad = historyMemCap

func ensureCallHistoryTable(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS call_history (
		call_id    TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		ended_at   BIGINT NOT NULL,
		record     JSONB NOT NULL
	)`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS call_history_session_ended ON call_history (session_id, ended_at DESC)`)
	return err
}

func (s *sessionStore) saveCall(ctx context.Context, rec CallRecord) error {
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	var ended int64
	if rec.EndedAt != nil {
		ended = *rec.EndedAt
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO call_history (call_id, session_id, ended_at, record)
		VALUES ($1,$2,$3,$4) ON CONFLICT (call_id) DO UPDATE SET ended_at=EXCLUDED.ended_at, record=EXCLUDED.record`,
		rec.CallID, rec.SessionID, ended, raw)
	return err
}

// recentCalls devolve as últimas n chamadas encerradas, da mais antiga para a mais nova.
func (s *sessionStore) recentCalls(ctx context.Context, n int) ([]CallRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT record FROM call_history ORDER BY ended_at DESC LIMIT $1`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CallRecord
	for rows.Next() {
		var raw []byte
		var rec CallRecord
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &rec) == nil {
			out = append(out, rec)
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}
