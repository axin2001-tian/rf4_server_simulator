// dec 临时调试工具：把 trace 里的响应 hex 解码成字段树。
// 用法: go run ./cmd/dec <hex>
package main

import (
	"encoding/hex"
	"fmt"
	"os"

	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

func dump(v *proto.Value, ind int) {
	if v == nil {
		fmt.Printf("%s<nil>\n", pad(ind))
		return
	}
	p := pad(ind)
	switch v.Kind {
	case proto.KindObj:
		fmt.Printf("%sobj type=%d class=%s fields=%d\n", p, v.TypeID, v.Class, len(v.Fields))
		for i, f := range v.Fields {
			fmt.Printf("%s [%d] op=%s ", p, i, f.Op)
			switch {
			case f.Str != nil:
				fmt.Printf("str=%q\n", *f.Str)
			case f.Val != nil:
				fmt.Println()
				dump(f.Val, ind+2)
			default:
				fmt.Printf("num=%v hex=%s\n", num2(&f), f.Hex)
			}
		}
	default:
		fmt.Printf("%skind=%s type=%d num=%v str=%v hex=%s items=%d\n", p, v.Kind, v.TypeID, v.Num, v.Str, v.Hex, len(v.Items))
		for _, it := range v.Items {
			dump(it, ind+1)
		}
	}
}

func num2(f *proto.Field) uint64 {
	if f.Num == nil {
		return 0
	}
	return *f.Num
}

func pad(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += "  "
	}
	return s
}

func main() {
	b, err := hex.DecodeString(os.Args[1])
	if err != nil {
		fmt.Println("hex err:", err)
		return
	}
	v, n, err := proto.DecodeValue(b, 0, wiregen.Table())
	fmt.Printf("consumed=%d/%d err=%v\n", n, len(b), err)
	dump(v, 0)
}
