# reader

`reader` は、Markdown / テキストファイルをローカルブラウザで読むための軽量ビューアです。

ファイルツリー、Markdown 表示、相対リンク/画像の解決に対応しています。Git リポジトリ内では、`main` ブランチとの差分表示も利用できます。`-write` を指定すると、指定したディレクトリを編集用ツリーとして開き、ブラウザから保存できます。`-pull-requests` を指定すると、GitHub CLI (`gh`) で取得できる場合だけサイドバーに自分の作業が必要な Pull Request も表示します。

## インストール

```bash
go install github.com/rubellum/reader@v0.9.0
```

## 使い方

```bash
reader [options] [directory]
```

`directory` には閲覧したいディレクトリを指定します。省略するとカレントディレクトリを開きます。起動後、デフォルトでは `http://127.0.0.1:3333` をブラウザで開きます。

```bash
reader
reader ./docs
reader -port 8080 ./docs
reader -host 0.0.0.0 -port 8080 ./docs
reader -include "*.md" -exclude "draft/*" ./docs
reader -read /path/to/reference -read-r /path/to/archive ./repo
reader -write /path/to/notes ./repo
reader -archive archived ./repo
reader -write /path/to/notes -write-r /path/to/archive ./repo
reader -pull-requests ./repo
reader -no-open ./repo
```

`-read` / `-write` で指定した root のファイルをアーカイブすると、相対 `-archive` は起動ディレクトリ基準で解決され、root 名を含めたパスへ移動します。

```bash
cd /path/to
reader -read test
# test/a.md をアーカイブすると archive/test/a.md に移動します。
```

## よく使うオプション

- `-host`: バインドアドレスです。デフォルトは `127.0.0.1` です。
- `-port`: ポート番号。デフォルトは `3333` です。使用中の場合は後続の空きポートへフォールバックします。
- `-include`: 表示するファイルの glob パターンです。複数指定できます。
- `-exclude`: 除外するファイルの glob パターンです。複数指定できます。
- `-read`: 閲覧ツリーに表示するディレクトリです。複数指定できます。指定時は起動ディレクトリを閲覧ツリーに表示しません。
- `-read-r`: 閲覧ツリーに表示するディレクトリです。複数指定できます。指定したディレクトリのサイドバーの並び順を降順にします。
- `-write`: 編集ツリーに表示するディレクトリです。複数指定できます。指定時に編集 UI が有効になります。
- `-write-r`: 編集ツリーに表示するディレクトリです。複数指定できます。指定したディレクトリのサイドバーの並び順を降順にします。
- `-archive`: アーカイブフォルダです。デフォルトは `archive` で、相対パスの場合は起動ディレクトリ基準です。`-read` / `-write` の root 配下のファイルは root 名を含め、ファイルの相対パス構造を保ったまま移動します。
- `-pull-requests`: GitHub Pull Request 一覧を表示します。`gh` で取得できない場合は表示しません。
- `-config`: JSON 設定ファイルを指定します。未指定時は `./config.json` があれば読み込みます。
- `-no-open`: 起動時にブラウザを自動で開きません。
- `-v`: 詳細ログを出します。`-vv` / `-vvv` も指定できます。

`-include` と `-exclude` のどちらも未指定の場合は、`*.md`, `*.txt`, `*.html`, `*.htm` を表示し、`node_modules`, `vendor`, `.git`, `dist`, `build`, `venv` などを除外します。HTML ファイルにはサイドバー上で新しいタブ用の URL リンクが表示されます。編集ツリーの HTML リンクもプレビュー用で、サイドバーから直接編集対象として開く操作には使いません。

## Pull Requests

`-pull-requests` を指定し、`gh` がインストール済みかつログイン済みの場合、サイドバーの `pull requests` に以下の open PR が表示されます。未指定時、または `gh` で取得できない場合、PR 一覧は表示されません。

- 自分にレビュー依頼が来ている PR
- 自分に assign されている PR
- 自分が作成し、変更依頼・チェック失敗・ブロック状態になっている PR

PR 一覧はサーバー側で1分キャッシュされるため、画面更新やポーリングで頻繁に GitHub API を呼びません。既存のファイル未読管理と同様に、初回表示時点のPRは既読扱いになり、その後に新規追加または更新されたPRだけが未読になります。

## 設定ファイル

CLI 未指定の値は JSON 設定ファイルから読み込めます。CLI で指定した値が設定ファイルより優先されます。

```json
{
  "host": "127.0.0.1",
  "port": 3333,
  "include": ["*.md", "*.txt", "*.html", "*.htm"],
  "exclude": ["draft/*"],
  "read": "/path/to/reference",
  "read-r": "/path/to/archive",
  "write": "/path/to/notes",
  "archive": "archive",
  "write-r": "/path/to/old-notes",
  "pull-requests": false,
  "verbosity": 1,
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
