package gameserver

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"rf4server/internal/proto"
	"rf4server/internal/wiregen"
)

// 声呐网格资产；文件定义各水域的网格几何、单元格顺序和采样参数。
//
//go:embed assets/sonar_grids_4_0_24799.json
var sonarGridsJSON []byte

const (
	sonarResponseClass = "efnipphkbce"
	sonarCellClass     = "gphnpomceak"
	sonarEchoClass     = "inhomdofcfb"
	sonarCellArgCode   = "15004"
	sonarEchoArgCode   = "15005"
)

type sonarGrid struct {
	topLeftX float64
	topLeftZ float64
	width    float64
	length   float64
	cellSize float64
}

type sonarProfile struct {
	depthMin float64
	depthMax float64
	sizeMed  float64
	sizeP90  float64
}

type sonarGridAsset struct {
	SchemaVersion int                  `json:"schema_version"`
	Profile       string               `json:"profile"`
	MapID         string               `json:"map_id"`
	SourceLevel   string               `json:"source_level"`
	SourceSHA256  string               `json:"source_sha256"`
	CellOrder     string               `json:"cell_order"`
	Grids         map[string][]float64 `json:"grids"`
}

var (
	sonarGridsOnce sync.Once
	sonarGrids     map[string]sonarGrid
	sonarGridsErr  error
	sonarSeedSeq   atomic.Uint64
)

func loadSonarGrids() (map[string]sonarGrid, error) {
	sonarGridsOnce.Do(func() {
		var asset sonarGridAsset
		if err := json.Unmarshal(sonarGridsJSON, &asset); err != nil {
			sonarGridsErr = fmt.Errorf("解析声呐网格资产: %w", err)
			return
		}
		grids := make(map[string]sonarGrid, len(asset.Grids))
		for systemID, raw := range asset.Grids {
			if systemID == "" || len(raw) != 5 {
				continue
			}
			candidate := sonarGrid{
				topLeftX: raw[0],
				topLeftZ: raw[1],
				width:    raw[2],
				length:   raw[3],
				cellSize: raw[4],
			}
			if !finiteSonarNumber(candidate.topLeftX) || !finiteSonarNumber(candidate.topLeftZ) ||
				!finiteSonarNumber(candidate.width) || !finiteSonarNumber(candidate.length) ||
				!finiteSonarNumber(candidate.cellSize) || candidate.width <= 0 ||
				candidate.length <= 0 || candidate.cellSize <= 0 {
				continue
			}
			grids[systemID] = candidate
		}
		if len(grids) == 0 {
			sonarGridsErr = fmt.Errorf("声呐网格资产没有有效水域")
			return
		}
		sonarGrids = grids
	})
	return sonarGrids, sonarGridsErr
}

func finiteSonarNumber(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func sonarGridDimensions(g sonarGrid) (cols, rows int) {
	cols = int(g.width / g.cellSize)
	rows = int(g.length / g.cellSize)
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	return cols, rows
}

// sonarProfileFor 读取水域的声呐采样档案；查询失败或没有档案时返回 nil，
// 采样会使用默认区间。
func (s *Server) sonarProfileFor(ctx context.Context, systemID string) *sonarProfile {
	if s == nil || s.DB == nil || systemID == "" {
		return nil
	}
	var depthMin, depthMax, sizeMed, sizeP90 sql.NullFloat64
	err := s.DB.SQL.QueryRowContext(ctx, `
		SELECT depth_min, depth_max, size_med, size_p90
		FROM sonar_profile WHERE system_id=?`, systemID).
		Scan(&depthMin, &depthMax, &sizeMed, &sizeP90)
	if err != nil {
		return nil
	}
	return &sonarProfile{
		depthMin: sonarProfileValue(depthMin),
		depthMax: sonarProfileValue(depthMax),
		sizeMed:  sonarProfileValue(sizeMed),
		sizeP90:  sonarProfileValue(sizeP90),
	}
}

func sonarProfileValue(v sql.NullFloat64) float64 {
	if !v.Valid || !finiteSonarNumber(v.Float64) {
		return 0
	}
	return v.Float64
}

func newSonarRand() *rand.Rand {
	seed := uint64(time.Now().UnixNano()) ^ sonarSeedSeq.Add(1)
	return rand.New(rand.NewSource(int64(seed)))
}

func sonarUniform(rng *rand.Rand, low, high float64) float64 {
	return low + (high-low)*rng.Float64()
}

// sonarTriangular 返回 [low, high] 区间内的三角分布采样。
func sonarTriangular(rng *rand.Rand, low, high, mode float64) float64 {
	if high <= low || mode <= low {
		return low
	}
	if mode >= high {
		return high
	}
	c := (mode - low) / (high - low)
	u := rng.Float64()
	if u < c {
		return low + math.Sqrt(u*(high-low)*(mode-low))
	}
	return high - math.Sqrt((1-u)*(high-low)*(high-mode))
}

// sampleSonarEcho 生成一条声呐回声的体长和深度。
// 有档案时使用深度与体长分位区间；无档案时使用默认的鱼体大小和深度范围。
func sampleSonarEcho(rng *rand.Rand, profile *sonarProfile) (sizeCM, depthM float64) {
	if profile != nil {
		depthLow := profile.depthMin
		if depthLow == 0 {
			depthLow = 0.8
		}
		depthLow = math.Max(0.1, depthLow)
		depthHigh := profile.depthMax
		if depthHigh == 0 {
			depthHigh = 5.5
		}
		depthHigh = math.Max(depthLow+0.5, depthHigh)
		depthM = sonarTriangular(rng, depthLow, depthHigh, depthLow+(depthHigh-depthLow)*0.25)

		med := profile.sizeMed
		if med == 0 {
			med = 4.0
		}
		med = math.Max(0.5, med)
		p90 := profile.sizeP90
		if p90 == 0 {
			p90 = med * 3
		}
		p90 = math.Max(med, p90)
		x := rng.Float64()
		switch {
		case x < 0.80:
			sizeCM = sonarUniform(rng, med*0.5, med*2.0)
		case x < 0.97:
			sizeCM = sonarUniform(rng, med*1.5, p90)
		default:
			sizeCM = sonarUniform(rng, p90, math.Max(p90, 97.0))
		}
		return sizeCM, math.Min(depthM, depthHigh)
	}

	if rng.Float64() < 0.12 {
		sizeCM = sonarUniform(rng, 60, 130)
	} else {
		sizeCM = sonarUniform(rng, 12, 60)
	}
	return sizeCM, sonarUniform(rng, 0.8, 5.5)
}

func makeSonarEcho(sizeCM, depthM float64) (*proto.Value, error) {
	return wiregen.MakeObj(sonarEchoClass, map[int]*proto.Value{
		0: proto.F32(float32(sizeCM)),
		1: proto.F32(float32(depthM)),
	})
}

// sonarValListMode 控制网格数组的编码形状：
// false = args(code)（0x03 + code + 计数 + 元素），true = VAL 列表（0x01 + 哨兵 + 元素）。
// 客户端自己的 14/8 上报用的是 VAL 列表，但服务端下发声呐时的形状还没确证，
// 用运行参数 sonar_vallist 切换，便于在不重启进程的情况下对比两种编码。
var sonarValListMode atomic.Bool

// sonarDenseMode 诊断开关：每格都放一条回波，用来判断客户端是否真的渲染了
// 服务端下发的数据（全格有回波仍然没鱼点，说明问题在数据形状而不是鱼群分布）。
var sonarDenseMode atomic.Bool

// sonarEchoValList 只把「格内回波数组」编码成 VAL 列表，外层网格仍用 args。
// 用于区分两种数组形状到底是哪一层不对。
var sonarEchoValList atomic.Bool

// sonarPatternMode 诊断开关：整片网格只在正中格放一条大回波（50cm / 20m），
// 其它格全空。用来确认客户端到底有没有渲染这份数据、以及渲染在哪个位置。
var sonarPatternMode atomic.Bool

func makeSonarCell(echoes []*proto.Value) (*proto.Value, error) {
	if sonarValListMode.Load() || sonarEchoValList.Load() {
		// 格内回波数组用 VAL 列表；空格也写成「空列表」而不是 null，
		// 避免客户端把 null 当成偏移、整片网格错位。
		return wiregen.MakeObj(sonarCellClass, map[int]*proto.Value{
			0: proto.ValList(echoes...),
		})
	}
	if len(echoes) == 0 {
		// 空格必须用通用 null（0x00）。
		// 客户端解码器没有 0xFF 分支——发 ElemNull 会让它抛
		// "NotImplementedException: Not implemented 255"（Player.log 实测发生在
		// TcpChannel::_fromSocket 解码声呐网格时），连接层解析失败 → 断线重连
		// → 上船流程中断（表现为上船黑屏）。
		return proto.Null(), nil
	}
	code := sonarEchoArgCode
	return wiregen.MakeObj(sonarCellClass, map[int]*proto.Value{
		0: proto.Args(&code, echoes...),
	})
}

func makeEmptySonarResponse(systemID string) (*proto.Value, error) {
	// 注意：这里不能带 args(15004)。客户端解码 args 时会先按 code 解析元素类型，
	// 4.0.25034 的 OpenTypeResolver 没有 15004，哪怕 0 个元素也会抛
	// "GetType not found: 15004" 并断开连接（2026-11-04 09:59 实测）。
	// 因此空网格直接给 f1=null。
	return wiregen.MakeObj(sonarResponseClass, map[int]*proto.Value{
		0: proto.StrVal(systemID),
		1: proto.Null(),
	})
}

// synthesizeSonar 生成声呐网格：2 到 5 个鱼群中心向邻近格扩散，
// 空格编码为通用 null（0x00，客户端不认 0xFF）；未知水域返回 15003 空数组。
func synthesizeSonar(systemID string, profile *sonarProfile, rng *rand.Rand) (*proto.Value, error) {
	grids, err := loadSonarGrids()
	if err != nil {
		return nil, err
	}
	grid, ok := grids[systemID]
	if !ok {
		return makeEmptySonarResponse(systemID)
	}
	if rng == nil {
		rng = newSonarRand()
	}

	cols, rows := sonarGridDimensions(grid)
	cellCount := cols * rows
	hot := make(map[int]int)
	for clusters := rng.Intn(4) + 2; clusters > 0; clusters-- {
		center := rng.Intn(cellCount)
		for spreadCount := rng.Intn(12) + 3; spreadCount > 0; spreadCount-- {
			spread := center + (rng.Intn(4*cols+1) - 2*cols) + (rng.Intn(5) - 2)
			if spread >= 0 && spread < cellCount {
				hot[spread]++
			}
		}
	}

	items := make([]*proto.Value, 0, cellCount)
	dense := sonarDenseMode.Load()
	pattern := sonarPatternMode.Load()
	for idx := 0; idx < cellCount; idx++ {
		var echoes []*proto.Value
		echoCount := 0
		if pattern {
			// 诊断用：只在中列固定深度放一条大回波，用来确认客户端是否渲染、
			// 以及渲在哪个位置。其它格全空。
			if idx == (rows/2)*cols+cols/2 {
				echoCount = 1
			}
		} else if dense {
			echoCount = 1 // 诊断用：每格都放一条回波，用来确认客户端是否真的在用这份数据
		} else if hot[idx] > 0 {
			echoCount = rng.Intn(4) + 1
		}
		for echoCount > 0 {
			sizeCM, depthM := sampleSonarEcho(rng, profile)
			if pattern {
				sizeCM, depthM = 50, 20
			}
			echo, err := makeSonarEcho(sizeCM, depthM)
			if err != nil {
				return nil, err
			}
			echoes = append(echoes, echo)
			echoCount--
		}
		cell, err := makeSonarCell(echoes)
		if err != nil {
			return nil, err
		}
		items = append(items, cell)
	}

	if sonarValListMode.Load() {
		return wiregen.MakeObj(sonarResponseClass, map[int]*proto.Value{
			0: proto.StrVal(systemID),
			1: proto.ValList(items...),
		})
	}
	code := sonarCellArgCode
	return wiregen.MakeObj(sonarResponseClass, map[int]*proto.Value{
		0: proto.StrVal(systemID),
		1: proto.Args(&code, items...),
	})
}

func (s *Server) sonarResponse(ctx context.Context, systemID string) (*proto.Value, error) {
	// 客户端 4.0.25034 的 OpenTypeResolver 没有注册 15004：一旦 1/10 里带回
	// 真实的声呐网格（args(15004) 里的格子对象），客户端解码 TcpChannel 时会抛
	// "OpenTypeResolver.GetType not found: 15004"，连接层解析失败后主动断开，
	// 表现为上船/切图后每隔几秒重连一次（Player.log 2026-11-04 09:50:23 实测）。
	// 默认只回 15003 空网格；确实需要声呐回波时用运行参数 sonar_echo=1 打开。
	if s.tunableF("sonar_echo", 0) < 1 {
		return makeEmptySonarResponse(systemID)
	}
	sonarMode := s.tunableF("sonar_vallist", 0)
	sonarValListMode.Store(sonarMode == 1)
	sonarEchoValList.Store(sonarMode == 2)
	sonarDenseMode.Store(s.tunableF("sonar_dense", 0) >= 1)
	sonarPatternMode.Store(s.tunableF("sonar_pattern", 0) >= 1)
	return synthesizeSonar(systemID, s.sonarProfileFor(ctx, systemID), newSonarRand())
}
