package gameserver

// AdminResolveLevelXP 管理后台 player_update 的等级/经验归一：
//   - 带 level 不带 xp：xp 回填 baseXp(level)（改等级即改累计门槛）；
//   - 带 xp 不带 level：xp 生效，等级由累计经验重算托底（只升不降）；
//   - 两者都带：保留显式值并做一致性钳制；
//   - 都不带：维持现值。
//
// 一致性约束：
//
//	level = max(level, levelForXp(xp))   —— 等级不低于累计经验支撑的等级；
//	xp    = max(xp,    baseXp(level))    —— 经验不低于等级的累计门槛。
func AdminResolveLevelXP(levelReq, xpReq *int64, curLevel, curXp int64) (level, xp int64) {
	level, xp = curLevel, curXp
	if levelReq != nil {
		level = *levelReq
	}
	if xpReq != nil {
		xp = *xpReq
	}
	if xpReq == nil && levelReq != nil && level != curLevel {
		// 只改等级（且等级真的变了）时，将经验对齐到该等级的累计门槛。
		// 等级未变时不重排：后台表单可能是打开面板时的旧快照，
		// 重排会把玩家已获得的经验回滚到门槛值（客户端会重复弹"新级别"）。
		xp = baseXp(level)
	}
	if level < 1 {
		level = 1
	}
	if level > maxLevel {
		level = maxLevel
	}
	if lv := levelForXp(xp); lv > level {
		level = lv
	}
	if b := baseXp(level); xp < b {
		xp = b
	}
	// 经验回滚保护：等级未变而 xp 变小，视为过期表单快照，维持现值。
	// 经验模型单调只增；确需降级时请同时显式给出目标等级。
	if level == curLevel && xp < curXp {
		xp = curXp
	}
	return level, xp
}
