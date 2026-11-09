package gameserver

import (
	"bytes"
	"testing"

	"rf4server/internal/config"
	"rf4server/internal/logging"
	"rf4server/internal/proto"
)

// TestFishLandedKVText 上鱼播报 KV 文案：三档键1 都只放中文鱼名、键2 放
// 重量克，前缀由客户端本地化模板补（达标 kind3 无前缀）。
func TestFishLandedKVText(t *testing.T) {
	srv := NewServer(config.Game{}, logging.Discard(), nil)
	sess := newProbeSess(srv)
	table := sess.Table()
	f := &RolledFish{Species: "g_tench", Grade: 2, WeightG: 425}

	enc := func(kind int) []byte {
		b, err := proto.EncodeValue(srv.fishLandedKV(f, kind), table)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, kind := range []int{25, 26, 3} {
		if b := enc(kind); bytes.Contains(b, []byte("钓获")) {
			t.Fatalf("kind=%d 不应自带前缀: %x", kind, b)
		}
	}
	// 重量 425g 原样进键2（客户端按克/公斤显示）。
	if b := enc(3); !bytes.Contains(b, []byte{0xa9, 0x01, 0x00, 0x00}) {
		t.Fatalf("键2 重量应为 425 克: %x", b)
	}
}

// TestFishTVShape30MatchesProduction 回归：面板 kv_shape=30 的编码字节必须与
// 生产 fishTVKv 完全一致（typed 数组 "135"）。否则面板探测的结果代表不了
// 线上行为——旧实现把 Items 重包成 KindKVList，编码器落到硬编码的
// dejdolfnooh 类型名，字节与生产分叉。
func TestFishTVShape30MatchesProduction(t *testing.T) {
	srv := NewServer(config.Game{}, logging.Discard(), nil)
	sess := newProbeSess(srv)
	table := sess.Table()

	prod, err := proto.EncodeValue(fishTVKv("g_tench", 3280), table)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := proto.EncodeValue(fishTVKvShape(30, "g_tench", 3280), table)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(prod, probe) {
		t.Fatalf("shape30 bytes differ from production fishTVKv:\n prod=%x\nprobe=%x", prod, probe)
	}
	if !bytes.Contains(probe, []byte("135")) {
		t.Fatalf("shape30 must use typed array type name 135, got %x", probe)
	}
}
