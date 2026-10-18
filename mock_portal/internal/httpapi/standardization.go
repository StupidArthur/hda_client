package httpapi

import (
	"mime"
	"net/http"
	"strconv"
	"sync"

	"mock_portal/internal/bundle"
)

type guideSection struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

type bundleInfo struct {
	Name     string        `json:"name"`
	URL      string        `json:"url"`
	Size     int64         `json:"size"`
	Contents []bundle.Item `json:"contents"`
}

type guideResponse struct {
	Title    string         `json:"title"`
	Intro    string         `json:"intro"`
	Sections []guideSection `json:"sections"`
	Bundle   bundleInfo     `json:"bundle"`
}

var (
	bundleOnce sync.Once
	bundleZip  []byte
	bundleErr  error
)

func bundleBytes() ([]byte, error) {
	bundleOnce.Do(func() {
		bundleZip, bundleErr = bundle.Bytes()
	})
	return bundleZip, bundleErr
}

func (s *Server) handleStandardization(w http.ResponseWriter, r *http.Request) {
	items, err := bundle.Items()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "工具包读取失败")
		return
	}
	size := int64(0)
	if data, err := bundleBytes(); err == nil {
		size = int64(len(data))
	}
	writeJSON(w, http.StatusOK, guideResponse{
		Title: "标准化指南",
		Intro: "门户只提供下载。数据整理在本地完成，通过现有渠道交付；门户不会接收上传，也不会在线修改 Mock 配置。",
		Sections: []guideSection{
			{
				Heading: "1. 下载并使用 Skill",
				Body: "下载右侧工具包，把整个目录交给负责整理的 Agent 或人员。入口是 `skills/hda-data-import/SKILL.md`；" +
					"包内同时包含它引用的 v1.5.0 规则、升级说明和参考脚本，不要只转发一个 `SKILL.md`。\n\n" +
					"先读 `docs/HDA_MOCKER_4_RELEASE_1_5_0.md`；与旧版描述冲突时以统一版为准。",
			},
			{
				Heading: "2. 告诉 Agent 源时区",
				Body: "源数据的时间列不会自带业务时区。必须明确说明源时区（例如东八区），由整理方转换为 UTC 后写入 Parquet；" +
					"不能凭文件名猜测。HDA 的时间必须升序、无重复；DA 可以不带时间列按固定 `period_ms` 播放，" +
					"也可以带 UTC 时间轴按源时间差播放。两种模式不能混用。",
			},
			{
				Heading: "3. HDA 与 DA 的用途",
				Body: "- **HDA**：预存历史，供客户端按时间区间读取。第一列必须是带 UTC 时区的 `Timestamp`。\n" +
					"- **DA**：实时轮播数据，按 `playback.files.<dataset>.period_ms` 发布。\n" +
					"- 同时存在时，HDA/DA 的基础位号集合必须一致；也允许纯 HDA 或纯 DA。\n" +
					"- 服务端只读取 `hda/` 和 `da/` 下的 Parquet，原始 CSV、脚本和 manifest 不参与运行。",
			},
			{
				Heading: "4. 空值与质量码",
				Body: "数值为空、NaN 或 Inf 且未标 Bad 时表示**无观测**：HDA 跳过该点，DA 保持前值和前质量。" +
					"需要保留异常观测时，必须显式写入 Bad，不能依赖空数值。\n\n" +
					"状态列使用完整 OPC UA StatusCode（UInt32，0..4294967295）；状态单元格不能为 NULL。" +
					"列缺失才使用默认 `0`（StatusGood）；DA 的 `.__hda_status` 缺失时继承 DA 实际观测质量。" +
					"BASF 的对齐空格不能全部改成 Bad；宁波已转换过的 192/0 不能再次映射。",
			},
			{
				Heading: "5. 交付前验证",
				Body: "至少检查：文件存在、列名非空且唯一、HDA/DA 基础位号一致、辅助列都有对应基础位号、" +
					"状态值在合法范围、HDA 时间单调、采样间隔符合预期、时间为 UTC、数值可转换为 float。" +
					"验证通过后再复制到生产 preset 目录，并同步更新 `config.yaml` 的 `playback.files`。\n\n" +
					"整理完成后通过现有渠道交付；门户不会自动创建 preset、上传文件或启动 Mock。",
			},
		},
		Bundle: bundleInfo{
			Name:     bundle.ArchiveName,
			URL:      "/api/standardization/bundle",
			Size:     size,
			Contents: items,
		},
	})
}

func (s *Server) handleBundle(w http.ResponseWriter, r *http.Request) {
	data, err := bundleBytes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "工具包读取失败")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": bundle.ArchiveName}))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
