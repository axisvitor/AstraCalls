package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

// Testes contra um Postgres de verdade: rodam só com WACALLS_TEST_PG_URL setado
// (ex.: postgres://postgres:test@127.0.0.1:55432/postgres?sslmode=disable).
func testPG(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("WACALLS_TEST_PG_URL")
	if url == "" {
		t.Skip("WACALLS_TEST_PG_URL não setado")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS call_history, agents`)
		db.Close()
	})
	_, _ = db.Exec(`DROP TABLE IF EXISTS call_history, agents`)
	return db
}

// O histórico gravado volta igual (e na ordem) depois de "reiniciar".
func TestPGCallHistoryPersists(t *testing.T) {
	db := testPG(t)
	ctx := context.Background()
	if err := ensureCallHistoryTable(ctx, db); err != nil {
		t.Fatal(err)
	}
	st := &sessionStore{db: db}
	for i, id := range []string{"a", "b", "c"} {
		end := int64(1000 + i)
		if err := st.saveCall(ctx, CallRecord{SessionID: "s1", CallID: id, Direction: "inbound", Missed: id == "b", Agent: "Maria", EndedAt: &end}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := st.recentCalls(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].CallID != "b" || rows[1].CallID != "c" || !rows[0].Missed || rows[0].Agent != "Maria" {
		t.Fatalf("recentCalls = %+v", rows)
	}
}

// Atendente criado sobrevive a reinício (diretório recarregado do banco).
func TestPGAgentsPersist(t *testing.T) {
	db := testPG(t)
	ctx := context.Background()
	t.Setenv("WACALLS_API_KEY", "mestra")
	d, err := newAgentDirectory(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	a, err := d.create(ctx, "joao", "João", "senha-forte-1", []string{"s1", "s2"})
	if err != nil {
		t.Fatal(err)
	}
	name := "João Silva"
	if _, err := d.update(ctx, a.ID, &name, nil, nil); err != nil {
		t.Fatal(err)
	}
	d2, err := newAgentDirectory(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	_, got, _, err := d2.login("joao", "senha-forte-1", "198.51.100.1")
	if err != nil || got.Name != name || len(got.Sessions) != 2 {
		t.Fatalf("login após reinício: %+v %v", got, err)
	}
	if err := d2.remove(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	d3, _ := newAgentDirectory(ctx, db)
	if len(d3.list()) != 0 {
		t.Fatal("atendente apagado voltou")
	}
}
