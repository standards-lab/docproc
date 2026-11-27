//go:build linux

package stages_test

import (
	"os"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/email"
	"github.com/standards-lab/docproc/internal/tooltest"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/docproc/stages"
	"github.com/standards-lab/docproc/tool"
)

func standinEmailStage(t *testing.T, s onenoteSettings) *stages.EmailStage {
	t.Helper()
	spec := email.MailSpec()
	spec.Program = tooltest.Link(t, t.TempDir(), "docproc-mail", s)
	tl, err := tool.NewExec(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	r, err := email.NewReader(email.Config{Mail: tl})
	if err != nil {
		t.Fatal(err)
	}
	return stages.NewEmailStage(stages.EmailConfig{Reader: r, Version: stages.Versions(tl)})
}

func TestEmailStageThreading(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, inReplyTo, references string
	}{
		{"mime/eml/09-threading", "<table-1@example.org>", "<notes-1@example.org> <table-1@example.org>"},
		{"generated/eml/01-plain", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			lines, err := os.ReadFile(tooltest.Tools(t, "mail/tests/golden/"+c.name+".jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			fixture := tooltest.Tools(t, "mail/tests/fixtures/"+c.name+".eml")
			stage := standinEmailStage(t, onenoteSettings{Stdout: string(lines)})
			d := newDispatcher(t, dispatch.Stages{route.Nested: stage}, dispatch.NewMemoryIndex(0))
			events := dispatchAll(t, d, fixture, nil)
			wantSteps(t, events, []step{{events[0].Part.Name, 0, dispatch.Prepared}})
			for _, kind := range []dispatch.OutputKind{dispatch.RecordOutput, dispatch.MarkdownOutput} {
				meta := outputNamed(t, events[0], kind, "message").Meta
				inReplyTo, hasInReplyTo := meta[dispatch.MetaInReplyTo]
				references, hasReferences := meta[dispatch.MetaReferences]
				if inReplyTo != c.inReplyTo || hasInReplyTo != (c.inReplyTo != "") ||
					references != c.references || hasReferences != (c.references != "") {
					t.Errorf("%s message's meta %v, want in_reply_to %q and references %q", kind, meta, c.inReplyTo, c.references)
				}
			}
		})
	}
}
