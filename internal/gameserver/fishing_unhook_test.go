package gameserver

import (
	"context"
	"testing"

	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// 14/12 的 f0=8 是客户端上报"鱼脱钩离开"（实测值，不是注释里写的 5）。
// 只要饵还在水里，这一杆不应作废：清掉当前鱼、保留 CastActive、重新排咬钩。
func TestFishingLeftKeepsCastAfterUnhook(t *testing.T) {
	srv, db := newDetachTestServer(t)
	const uid = int64(1000001)
	// 把咬钩延迟拉长，避免测试结束后计时器才触发。
	if err := db.TunableSet(context.Background(), "bite_delay_fixed", "3600", "test"); err != nil {
		t.Fatal(err)
	}
	st := srv.stateFor(uid)
	st.mu.Lock()
	st.Pending = &RolledFish{FishGUID: "88888888888888888888888888888888", Species: "c.roach"}
	st.CastActive = true
	st.InWater = true
	st.Hooked = true
	st.mu.Unlock()

	obj := proto.Obj("lhicjpmglbo", 0, proto.Field{Op: "U32", Num: ptrU64(8)})
	tree := proto.Args524(obj)
	sess := newProbeSess(srv)
	if _, ok := hFishingLeft(&Ctx{Sess: sess, Main: 14, Sub: 12, Tree: tree, DecodeOK: true, Log: logging.Discard()}); !ok {
		t.Fatal("14/12 未处理")
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.CastActive {
		t.Fatal("脱钩后饵还在水里，这一杆应保持有效")
	}
	if st.Resolved {
		t.Fatal("脱钩后本轮应作废，但不该结束这一杆")
	}
	if st.Pending != nil {
		t.Fatal("脱钩后不应保留上一条鱼")
	}
	if st.biteTimer == nil {
		t.Fatal("脱钩后应重新安排咬钩计时")
	}
}

// 钓组已经离开水面时脱钩，这一杆正常结束，不再排咬钩。
func TestFishingLeftEndsCastWhenOutOfWater(t *testing.T) {
	srv, _ := newDetachTestServer(t)
	const uid = int64(1000001)
	st := srv.stateFor(uid)
	st.mu.Lock()
	st.Pending = &RolledFish{FishGUID: "99999999999999999999999999999999", Species: "c.roach"}
	st.CastActive = true
	st.InWater = false
	st.Hooked = true
	st.mu.Unlock()

	obj := proto.Obj("lhicjpmglbo", 0, proto.Field{Op: "U32", Num: ptrU64(8)})
	tree := proto.Args524(obj)
	sess := newProbeSess(srv)
	if _, ok := hFishingLeft(&Ctx{Sess: sess, Main: 14, Sub: 12, Tree: tree, DecodeOK: true, Log: logging.Discard()}); !ok {
		t.Fatal("14/12 未处理")
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.CastActive {
		t.Fatal("线已出水，这一杆应结束")
	}
	if !st.Resolved {
		t.Fatal("线已出水，本轮应作废")
	}
	if st.biteTimer != nil {
		t.Fatal("线已出水，不应再排咬钩")
	}
}
