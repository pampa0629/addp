package executor

import (
	"context"
	"errors"
	"testing"

	plugin "github.com/addp/common/engine/plugin"
)

type evidenceSession struct {
	plugin.TableReadSession
	sources *plugin.QueryReadSet
	err     error
}

func (s *evidenceSession) ReadSet(context.Context) (*plugin.QueryReadSet, error) {
	return s.sources, s.err
}

func TestPipelineSourcesOnlyComeFromActualSessionAndSurviveProtection(t *testing.T) {
	actual, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(3, "schema", "outdoor", "source"))
	session := &evidenceSession{sources: actual}
	for _, reader := range []TableBatchReader{
		&nativeTableSessionBatchReader{session: session},
		&queryTableBatchReader{session: session},
		&protectedTableBatchReader{inner: &queryTableBatchReader{session: session}},
	} {
		proof := productionSourcesFromReader(t.Context(), reader)
		if proof == nil || proof.Paths[0].Segments[2].Name != "source" {
			t.Fatal("actual evidence lost through reader wrapper")
		}
		proof.Paths[0].Segments[2].Name = "changed"
		if actual.Paths[0].Segments[2].Name != "source" {
			t.Fatal("pipeline proof aliases the provider")
		}
	}
	for _, session := range []plugin.TableReadSession{
		&fakeQueryReadSession{},
		&evidenceSession{err: plugin.ErrQueryReadSetUnresolved},
		&evidenceSession{sources: &plugin.QueryReadSet{}},
		&evidenceSession{sources: &plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{actual.Paths[0], actual.Paths[0]}}},
	} {
		if sources := productionSourcesFromReader(t.Context(), &nativeTableSessionBatchReader{session: session}); sources != nil {
			t.Fatal("missing/malformed proof became production sources")
		}
	}
}

type changedReadSessionProvider struct {
	fakeQueryReadSessionProvider
	session *changedReadSession
}

type changedReadSession struct {
	fakeQueryReadSession
	sources *plugin.QueryReadSet
}

func (s *changedReadSession) ReadSet(context.Context) (*plugin.QueryReadSet, error) {
	return s.sources.Clone(), nil
}

func (p *changedReadSessionProvider) OpenQueryReadSession(context.Context, plugin.PreparedQuery) (plugin.QueryReadSession, error) {
	return p.session, nil
}

func TestQuerySourceRefusesDifferentActualSessionSources(t *testing.T) {
	expected, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(3, "schema", "outdoor", "declared"))
	actual, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(3, "schema", "outdoor", "actual"))
	session := &changedReadSession{sources: actual}
	provider := &changedReadSessionProvider{fakeQueryReadSessionProvider: fakeQueryReadSessionProvider{prepared: &fakePreparedQuery{readSet: expected}}, session: session}
	reader, err := (&queryTableBatchSource{provider: provider, expectedReadSet: expected}).Open(t.Context())
	if err == nil || reader != nil || !session.closed || errors.Is(err, plugin.ErrQueryReadSetUnresolved) {
		t.Fatal("changed real source did not close before target operations", err)
	}
}
