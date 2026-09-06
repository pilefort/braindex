# 知識カタログ(braindex 自動生成 — 手で編集しない)

生成: 2026-08-07 / 3 リポジトリ / 7 件
再生成: hub リポで `braindex` を実行
使い方: このファイルを grep → ヒット行のパス(root 相対)の実ファイルを読む。要旨だけで答えない

## group-a/repo-x
| 日付 | 種別 | タイトル | 要旨 | パス |
|---|---|---|---|---|
| 2026-08-03 | decisions | repo-x の決定（1 件） | 最初の決定 | group-a/repo-x/docs/decisions.md |
| 2026-08-02 | notes/project | X のプロジェクト知見 | 結論: 2 段目のリポの project ノート。種別は notes/project。 | group-a/repo-x/docs/notes/project/x-proj.md |
| 2026-08-01 | notes | X のノート | 結論: 2 段目のリポ(group-a/repo-x)の直下ノート。日付はファイル名から取る。 | group-a/repo-x/docs/notes/20260801_x-note.md |

## group-a/repo-y
| 日付 | 種別 | タイトル | 要旨 | パス |
|---|---|---|---|---|
| 2026-08-04 | notes | Y のノート | 結論: 同じ group の別リポ。H2 見出しは group-a/repo-y になる。 | group-a/repo-y/docs/notes/y-note.md |

## group-b/repo-z
| 日付 | 種別 | タイトル | 要旨 | パス |
|---|---|---|---|---|
| 2026-08-07 | research | 調査の入口 | 結論: 再帰 extra の起点直下は kind そのまま。 | group-b/repo-z/research/top.md |
| 2026-08-06 | research/topic | 調査メモ | 結論: extra(repo は group-b/repo-z)で拾う research のサブディレクトリ。 | group-b/repo-z/research/topic/20260806_paper.md |
| 2026-08-05 | notes/common | Z の共通知見 | 結論: 別 group のリポ。種別は notes/common。 | group-b/repo-z/docs/notes/common/z-common.md |
