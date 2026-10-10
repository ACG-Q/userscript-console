package layout

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsMatchLegacyHardcoded(t *testing.T) {
	d := Defaults()
	if d.Registry != "registry.json" || d.Scripts != "scripts" || d.Dist != "dist" || d.Archive != "archive/commands.json" {
		t.Errorf("默认布局必须与历史硬编码一致, 实际 %+v", d)
	}
}

func TestZeroValueLayoutBehavesLikeDefaults(t *testing.T) {
	var z Layout
	root := filepath.Join("data")
	if got := z.RegistryPath(root); got != filepath.Join(root, "registry.json") {
		t.Errorf("零值 RegistryPath = %q", got)
	}
	if got := z.ScriptsPath(root); got != filepath.Join(root, "scripts") {
		t.Errorf("零值 ScriptsPath = %q", got)
	}
	if got := z.DistPath(root); got != filepath.Join(root, "dist") {
		t.Errorf("零值 DistPath = %q", got)
	}
	if got := z.ArchivePath(root); got != filepath.Join(root, "archive", "commands.json") {
		t.Errorf("零值 ArchivePath = %q", got)
	}
	if got := z.DistSeg(); got != "dist" {
		t.Errorf("零值 DistSeg = %q", got)
	}
}

func TestFromEnvReadsEnvOrDefault(t *testing.T) {
	t.Setenv("USM_REGISTRY", "reg.json")
	t.Setenv("USM_SCRIPTS_DIR", "from-env")
	t.Setenv("USM_DIST_DIR", "")
	t.Setenv("USM_ARCHIVE_PATH", "")

	got := FromEnv()
	if got.Registry != "reg.json" {
		t.Errorf("USM_REGISTRY 应生效, 实际 %q", got.Registry)
	}
	if got.Scripts != "from-env" {
		t.Errorf("USM_SCRIPTS_DIR 应生效, 实际 %q", got.Scripts)
	}
	if got.Dist != "dist" {
		t.Errorf("空 env 应取默认, 实际 %q", got.Dist)
	}
	if got.Archive != "archive/commands.json" {
		t.Errorf("未设置 env 应取默认, 实际 %q", got.Archive)
	}
}

func TestFlagBeatsEnv(t *testing.T) {
	t.Setenv("USM_SCRIPTS_DIR", "from-env")
	t.Setenv("USM_REGISTRY", "env-reg.json")

	got, err := FromEnv().WithFlags("flag-reg.json", "flag-scripts", "", "")
	if err != nil {
		t.Fatalf("WithFlags 报错: %v", err)
	}
	if got.Registry != "flag-reg.json" {
		t.Errorf("flag 应覆盖 env, Registry = %q", got.Registry)
	}
	if got.Scripts != "flag-scripts" {
		t.Errorf("flag 应覆盖 env, Scripts = %q", got.Scripts)
	}
	if got.Dist != "dist" {
		t.Errorf("空 flag 应保留来源值, Dist = %q", got.Dist)
	}
	if got.Archive != "archive/commands.json" {
		t.Errorf("Archive 应取默认, 实际 %q", got.Archive)
	}
}

func TestWithFlagsRejectsUnsafeRelativePaths(t *testing.T) {
	cases := []struct {
		name string
		fn   func(Layout) (Layout, error)
	}{
		{"registry 含 ..", func(l Layout) (Layout, error) { return l.WithFlags("../evil.json", "", "", "") }},
		{"scripts 含 ..", func(l Layout) (Layout, error) { return l.WithFlags("", "../x", "", "") }},
		{"dist 含 ..", func(l Layout) (Layout, error) { return l.WithFlags("", "", "../x", "") }},
		{"archive 含 ..", func(l Layout) (Layout, error) { return l.WithFlags("", "", "", "../x") }},
		{"scripts 含反斜杠", func(l Layout) (Layout, error) { return l.WithFlags("", `a\b`, "", "") }},
		{"registry 深层 .. 段", func(l Layout) (Layout, error) { return l.WithFlags("a/../b.json", "", "", "") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if out, err := c.fn(Defaults()); err == nil {
				t.Errorf("应拒绝, 实际放行: %+v", out)
			}
		})
	}
}

func TestEmptyFieldsRejectedByWithFlags(t *testing.T) {
	// 零值 Layout 的空字段不是合法路径——WithFlags 是显式配置入口，须报错；
	// 零值容忍只发生在各 *Path()/DistSeg() 方法（normalize 兜底）。
	if _, err := (Layout{}).WithFlags("", "", "", ""); err == nil {
		t.Error("零值 Layout 经 WithFlags 应报空路径错误")
	}
}

func TestAbsolutePathsPassThrough(t *testing.T) {
	absReg := filepath.Join(filepath.VolumeName("C:")+string(filepath.Separator)+"abs", "registry.json")
	if !filepath.IsAbs(absReg) {
		t.Skipf("当前平台构造不出绝对路径: %q", absReg)
	}
	got, err := Defaults().WithFlags(absReg, "", "", "")
	if err != nil {
		t.Fatalf("绝对路径应放行: %v", err)
	}
	root := filepath.Join("some", "root")
	if p := got.RegistryPath(root); p != absReg {
		t.Errorf("绝对 Registry 应原样, got %q want %q", p, absReg)
	}
}

func TestDistSegCleansAndStripsSlashes(t *testing.T) {
	cases := []struct {
		dist string
		want string
	}{
		{"dist", "dist"},
		{"out/js", "out/js"},
		{"./bundle", "bundle"},
		{filepath.ToSlash(filepath.Join(string(filepath.Separator)+"abs", "dist")), "abs/dist"},
	}
	for _, c := range cases {
		got, err := Defaults().WithFlags("", "", c.dist, "")
		if err != nil {
			t.Fatalf("dist %q 被拒: %v", c.dist, err)
		}
		if seg := got.DistSeg(); seg != c.want {
			t.Errorf("DistSeg(%q) = %q, want %q", c.dist, seg, c.want)
		}
	}
}

func TestPathMethodsJoinSlashRelativePaths(t *testing.T) {
	// 相对值统一用 "/"，落到各平台时转系统分隔符（SPEC-DATA 跨平台契约）。
	got := Defaults().RegistryPath("data")
	if got != filepath.Join("data", "registry.json") {
		t.Errorf("RegistryPath = %q", got)
	}
	if strings.ContainsRune(got, '/') && filepath.Separator == '\\' {
		t.Errorf("Windows 下应转为系统分隔符, got %q", got)
	}
}
