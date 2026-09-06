# 索引データと Markdown の互換性

索引の共通型は `internal/indexdata.Entry`、保存済み索引の読み取りは
`indexdata.ParseCatalog` が担当する。描画は引き続き `render.Render` が担当する。
ファイルの正本や設定は増やさず、既存の `catalog.md` の書式を維持する。

`catalog.Build` の結果は、表示用の `Catalog`、件数の `Entries`、
構造化した行の `Records`、読取警告の `Warnings` を持つ。
同一処理内では `Records` を渡し、保存済みファイルだけを ParseCatalog で読む。
Records は走査順であり、表示順を必要とする利用側は描画側と同じ規則で並べる。

Records のタイトル・要旨は既存の表と同じく半角パイプを全角へ置換し、
各列の前後空白を除く。これにより、保存済みの索引との比較で見かけの変更を増やさない。
この正規化は元ノートを書き換えない。元の文字列を復元する用途には使えない。

旧 `render.Entry` は共通型の別名、旧 `review.ParseCatalog` は共通の読み取り処理を
呼ぶ互換入口として残す。既存利用側の一括移行は不要。
読取時の BOM・改行の正規化、空入力、壊れた表のエラーも従来の扱いを維持する。
