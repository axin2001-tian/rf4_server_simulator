package gameserver

import (
	"testing"

	"rf4server/internal/proto"
)

func callSkillCommand(t *testing.T, srv *Server, sess *Session, sub uint8, args *proto.Value) uint64 {
	t.Helper()
	body, ok := (&handlerBox{srv: srv, sess: sess, main: 5, sub: sub, tree: args}).invoke(t)
	if !ok {
		t.Fatalf("5/%d 未被处理", sub)
	}
	return treeAvailPoints(t, decodeTreeValue(t, srv, body))
}

func TestSkillGroupAndLinkShareLevels(t *testing.T) {
	srv := sharedSkillTestServer(t, 10)
	ctx := t.Context()
	sess := &Session{ID: 1, Userid: sharedSkillUID, Srv: srv}
	if err := srv.DB.AddSkillPoints(ctx, sharedSkillUID, 2, 100); err != nil {
		t.Fatal(err)
	}
	if err := srv.DB.AddSkillPoints(ctx, sharedSkillUID, 11, 100); err != nil {
		t.Fatal(err)
	}

	if got := callSkillCommand(t, srv, sess, 2, proto.Args524(proto.U32(2), proto.U32(718))); got != 8 {
		t.Fatalf("first learn available=%d", got)
	}
	if got := callSkillCommand(t, srv, sess, 2, proto.Args524(proto.U32(2), proto.U32(721))); got != 7 {
		t.Fatalf("same-group learn available=%d", got)
	}
	if got := callSkillCommand(t, srv, sess, 2, proto.Args524(proto.U32(11), proto.U32(1008))); got != 6 {
		t.Fatalf("cross-category learn available=%d", got)
	}
	for _, node := range []int64{718, 721, 1008, 1020} {
		if rank := sharedSkillRank(t, srv, node); rank != 3 {
			t.Fatalf("node %d rank=%d", node, rank)
		}
	}
	if spent := srv.skillSpentPoints(ctx, sharedSkillUID); spent != 3 {
		t.Fatalf("spent=%d", spent)
	}
	tree, err := srv.BuildSkillTree311(ctx, sharedSkillUID, ^uint64(0))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		cat  int64
		node int64
	}{{2, 718}, {2, 721}, {11, 1008}, {11, 1020}} {
		cat := sharedSkillCategoryForTest(t, tree, item.cat)
		if level := u32Of(sharedSkillNodeForTest(t, cat, item.node).Field(3).Num); level != 3 {
			t.Fatalf("tree cat=%d node=%d level=%d", item.cat, item.node, level)
		}
	}
}

func TestSkillResetByCategoryAndNode(t *testing.T) {
	srv := sharedSkillTestServer(t, 10)
	sess := &Session{ID: 1, Userid: sharedSkillUID, Srv: srv}

	if got := callSkillCommand(t, srv, sess, 2, proto.Args524(proto.U32(1), proto.U32(514))); got != 8 {
		t.Fatalf("learn available=%d", got)
	}
	for _, node := range []int64{514, 701, 313, 1002} {
		if rank := sharedSkillRank(t, srv, node); rank != 1 {
			t.Fatalf("linked node %d rank=%d", node, rank)
		}
	}
	if got := callSkillCommand(t, srv, sess, 3, proto.Args524(proto.U32(1), proto.U32(514))); got != 9 {
		t.Fatalf("node reset available=%d", got)
	}
	for _, node := range []int64{514, 701, 313, 1002} {
		if rank := sharedSkillRank(t, srv, node); rank != 0 {
			t.Fatalf("node reset left %d=%d", node, rank)
		}
	}

	if got := callSkillCommand(t, srv, sess, 2, proto.Args524(proto.U32(1), proto.U32(514))); got != 8 {
		t.Fatalf("second learn available=%d", got)
	}
	if got := callSkillCommand(t, srv, sess, 3, proto.Args524(proto.U32(1))); got != 9 {
		t.Fatalf("category reset available=%d", got)
	}
	for _, node := range []int64{514, 701, 313, 1002} {
		if rank := sharedSkillRank(t, srv, node); rank != 0 {
			t.Fatalf("category reset left %d=%d", node, rank)
		}
	}
}
