# reader

`reader` は、Git リポジトリ内の Markdown / テキストファイルをローカルブラウザで読むための軽量ビューアです。

ファイルツリー、Markdown 表示、相対リンク/画像の解決、`main` ブランチとの差分表示に対応しています。`-write` を指定すると、指定したディレクトリを編集用ツリーとして開き、ブラウザから保存できます。

## インストール

```bash
go install github.com/rubellum/reader@v0.9.0
```

## 使い方

```bash
reader [options] [directory]
```

`directory` には Git リポジトリ内のディレクトリを指定します。省略するとカレントディレクトリを開きます。起動後、デフォルトでは `http://127.0.0.1:3333` をブラウザで開きます。

```bash
reader
reader ./docs
reader -port 8080 ./docs
reader -include "*.md" -exclude "draft/*" ./docs
reader -write /path/to/notes ./repo
reader -archive archived ./repo
reader -no-open ./repo
```

## よく使うオプション

- `-port`: ポート番号。デフォルトは `3333` です。使用中の場合は後続の空きポートへフォールバックします。
- `-include`: 表示するファイルの glob パターンです。複数指定できます。
- `-exclude`: 除外するファイルの glob パターンです。複数指定できます。
- `-read`: 閲覧ツリーに表示するディレクトリです。複数指定できます。指定時は起動ディレクトリを閲覧ツリーに表示しません。
- `-write`: 編集ツリーに表示するディレクトリです。複数指定できます。指定時に編集 UI が有効になります。
- `-archive`: アーカイブフォルダです。デフォルトは `archive` で、ファイルの相対パス構造を保ったまま移動します。
- `-config`: JSON 設定ファイルを指定します。未指定時は `./config.json` があれば読み込みます。
- `-no-open`: 起動時にブラウザを自動で開きません。
- `-v`: 詳細ログを出します。`-vv` / `-vvv` も指定できます。

`-include` と `-exclude` のどちらも未指定の場合は、`*.md` と `*.txt` のみを表示し、`node_modules`, `vendor`, `.git`, `dist`, `build`, `venv` などを除外します。

## 設定ファイル

CLI 未指定の値は JSON 設定ファイルから読み込めます。CLI で指定した値が設定ファイルより優先されます。

```json
{
  "port": 3333,
  "include": ["*.md", "*.txt"],
  "exclude": ["draft/*"],
  "read": "/path/to/reference",
  "write": "/path/to/notes",
  "archive": "archive",
  "dir": "/path/to/repo"
}
```

## 注意

- 認証機能はありません。信頼できるローカル環境で使用してください。
- サーバーはデフォルトで `127.0.0.1` にバインドされます。`-host 0.0.0.0` などを指定すると、同じネットワーク上の端末から閲覧 API や保存 API にアクセスできる可能性があります。
- 秘密情報を含むディレクトリを指定すると、表示対象ファイルはブラウザから読めます。
- `-write` を指定すると、対象ディレクトリ内の既存ファイルの更新と新規ファイル作成をブラウザから実行できます。

## ライセンス

MIT
