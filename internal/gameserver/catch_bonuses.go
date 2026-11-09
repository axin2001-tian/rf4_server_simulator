package gameserver

import (
	"context"
	"fmt"
	"math"
)

// catchXPBonus 随结算回执保存；重登和重放不按新装备重算奖励。
type catchXPBonus struct {
	Kind       uint32
	GUID       string
	Multiplier float64
	XP         int64
}

func (s *Server) rodXPBonuses(ctx context.Context, uid int64, rodGUID string, baseXP int64) ([]catchXPBonus, error) {
	if rodGUID == "" || rodGUID == zeroGUID || baseXP <= 0 {
		return nil, nil
	}
	row, err := s.DB.GetItemByGUID(ctx, rodGUID)
	if err != nil {
		return nil, err
	}
	if row == nil || row.UID != uid || row.Durability <= 0 {
		return nil, fmt.Errorf("经验结算的鱼竿无效")
	}
	def, err := s.itemDefForRow(ctx, row)
	if err != nil {
		return nil, err
	}
	if def == nil || def.Category != gearCatRod {
		return nil, fmt.Errorf("经验结算物品不是鱼竿")
	}
	entries, _ := defAttrs(def)["dmcgapifbdm"].([]any)
	var bonuses []catchXPBonus
	skillRate := 0.0
	for _, raw := range entries {
		b, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch b["fopbhlmkcpe"] {
		case float64(9):
			kind, ok := b["liampcgfchp"].(float64)
			if !ok || kind < 1 || kind > 3 || kind != math.Trunc(kind) {
				continue
			}
			rate, ok := decodeU32F32(b["kdhkgjcjpel"])
			if !ok || rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
				return nil, fmt.Errorf("鱼竿经验加成无效")
			}
			amount := math.Round(float64(baseXP) * rate)
			if amount >= float64(math.MaxInt64) {
				return nil, fmt.Errorf("鱼竿经验加成超出范围")
			}
			if amount > 0 {
				bonuses = append(bonuses, catchXPBonus{Kind: uint32(kind), GUID: row.GUID, Multiplier: rate, XP: int64(amount)})
			}
		case float64(3):
			// 鱼竿的「技能」奖金（如 +10% 技能「水底钓鱼」）也按同百分比计入渔具经验。
			pct, ok := decodeU32F32(b["oalocpjcokm"])
			if !ok || pct <= 0 || math.IsNaN(pct) || math.IsInf(pct, 0) {
				continue
			}
			skillRate += pct / 100
		}
	}
	if skillRate > 0 {
		amount := math.Round(float64(baseXP) * skillRate)
		if amount >= float64(math.MaxInt64) {
			return nil, fmt.Errorf("鱼竿经验加成超出范围")
		}
		if amount > 0 {
			bonuses = append(bonuses, catchXPBonus{Kind: 2, GUID: row.GUID, Multiplier: skillRate, XP: int64(amount)})
		}
	}
	return bonuses, nil
}
