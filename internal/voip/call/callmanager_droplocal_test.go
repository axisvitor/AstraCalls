package call

import (
	"testing"

	"wacalls/internal/voip/core"
)

// DropLocal encerra só deste lado: nenhum stanza (reject/terminate) sai.
func TestDropLocalSendsNothing(t *testing.T) {
	m := incomingRingingManager()
	sock := m.sock.(*fakeSock)
	m.DropLocal(core.EndCallReasonTimeout)
	if !m.CurrentCall().IsEnded() {
		t.Fatalf("chamada não encerrou: %s", m.CurrentCall().StateData.State)
	}
	if tags := sock.sentTags(); len(tags) != 0 {
		t.Fatalf("DropLocal enviou %v", tags)
	}
	m.DropLocal(core.EndCallReasonTimeout) // idempotente
}
