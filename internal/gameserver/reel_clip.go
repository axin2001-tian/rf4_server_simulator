package gameserver

import (
	"context"
	"strconv"

	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// 基础实现：轮管卡默认关闭，按竿持久化卡米数。
const (
	reelClipSettingKey = "reel_clip_m"
	reelDragSettingKey = "reel_drag_setting"
	maxReelClipM       = 10000
)

func clampReelClipM(v int64) int64 {
	if v < 0 {
		return 0
	}
	if v > maxReelClipM {
		return maxReelClipM
	}
	return v
}

func rigClipOf(v *proto.Value) int64 {
	if v == nil || v.Kind != proto.KindObj || v.FieldCount() <= 5 {
		return 0
	}
	f := v.Field(5)
	if f.Num == nil {
		return 0
	}
	return clampReelClipM(int64(u32Of(f.Num)))
}

func buildRig153WithClip(name string, rigType int64, comps map[int64]rigComp, depth, clipM int64) *proto.Value {
	if name == "" {
		name = "rig_FloatSimple"
	}
	return wiregen.MustMakeObj("gbbhdkefnkn", map[int]*proto.Value{
		0: proto.StrVal(name),
		1: proto.U32(uint32(rigType)),
		2: proto.Args(proto.S("81"), sortedRigCompItems(comps, rigType)...),
		3: proto.U32(0),
		4: proto.U32(uint32(depth)),
		5: proto.U32(uint32(clampReelClipM(clipM))),
		6: proto.U32(15),
		7: proto.U32(15),
		8: proto.U32(0),
		9: proto.U32(0),
	})
}

func (s *Server) savedReelClipM(ctx context.Context, rodGUID string) (int64, bool) {
	if s.DB == nil || rodGUID == "" {
		return 0, false
	}
	raw, err := s.DB.GetRodSetting(ctx, rodGUID, reelClipSettingKey)
	if err != nil || raw == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return clampReelClipM(v), true
}
