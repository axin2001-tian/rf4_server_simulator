package gameserver

import (
	"bytes"
	"testing"

	"rf4server/internal/proto"
)

// TestFishTVKvEncoding 验证 401.f4 播报参数按客户端可解析的形状编码：
// [0x03]["135"][u16 n] + n×([01][u32 400][键][值])，
// 键1 = 鱼种key（短字符串），键2 = 重量克（int，客户端除以 1000 转千克）。
func TestFishTVKvEncoding(t *testing.T) {
	raw, err := proto.EncodeValue(fishTVKv("g_tench", 3280), nil)
	if err != nil {
		t.Fatal(err)
	}
	if raw[0] != 0x03 {
		t.Fatalf("容器 marker 必须是 0x03（对象数组）: % x", raw)
	}
	if raw[1] != 3 || !bytes.HasPrefix(raw[2:], []byte("135")) {
		t.Fatalf("容器类型名必须是 135: % x", raw[:6])
	}
	off := 2 + 3
	if raw[off] != 2 || raw[off+1] != 0 { // u16 count = 2 LE
		t.Fatalf("元素数编码错误: % x", raw[off:off+2])
	}
	// 元素1：[01][400 LE][key=1][0x14 短串 g_tench]；元素2：[01][400 LE][key=2][0x0D int 3280]
	want := append([]byte{0x01, 0x90, 0x01, 0x00, 0x00, 0x01, 0x14, 0x07}, []byte("g_tench")...)
	want = append(want, 0x01, 0x90, 0x01, 0x00, 0x00, 0x02, 0x0D, 0xD0, 0x0C, 0x00, 0x00)
	if !bytes.Equal(raw[off+2:], want) {
		t.Fatalf("编码与实测可渲染形状不符:\n got % x\nwant % x", raw[off+2:], want)
	}
}
