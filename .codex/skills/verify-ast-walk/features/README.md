# AST walk verification map

SKILL.mdのLaunch後、各drive前にDoctorを通す。各featureの入口を独立したCLI processで直列実行する。証拠は`$OUT/drives/`、campaignは`$OUT/campaigns/`。性能比較と動作検証を区別する。

| Feature | CLI entry points |
|---|---|
| [Traversal equivalence](equivalence.md) | verify |
| [Isolated walk sample](sampling.md) | sample |
| [Daily comparison](daily.md) | prepare、run、完了後のrun再実行 |
| [Artifact inspection and analysis](analysis.md) | inspect、collect、collect --out、report |

未対応: decision/KPC計測、M1 size sweep/曲線生成、shuffle、ancestor/revisit、実parser/checker end-to-end。これらは現状のfeatureとして検証済みにしない。全体監査では4ファイルと各入口を対象にし、一つの成功で他のcoverageを代用しない。
