// ===== 閲覧側の状態 =====
let currentDocument = null;
let lastModifiedAt = null;
let currentReadRoot = 'read';
let currentWorktree = null;
let worktrees = [];
const worktreesByRoot = {};
let isDiffMode = false;
let currentDiffData = null;
let readRoots = [{ id: 'read', name: '', sortDesc: false }];
const readTreeDataByRoot = {};
let writeRoots = [];
const writeTreeDataByRoot = {};
let currentFilterDir = null;
let currentFilterKeyword = '';
const collapsedReadDirsByRoot = {};

// ===== 編集側の状態（-write 指定時のみ有効） =====
let writeEnabled = false;
let currentEditFile = null;
let currentEditRoot = 'write';
let editDirty = false;
const collapsedWriteDirs = new Set();
const collapsedWriteDirsByRoot = {};

// URL操作関数

// URLパラメータを読み取る
function getURLParams() {
  const params = new URLSearchParams(window.location.search);
  return {
    file: params.get('file'),
    worktree: params.get('worktree'),
    root: params.get('root'),
    editFile: params.get('editFile'),
    editRoot: params.get('editRoot')
  };
}

function rootQuery(rootName) {
  if (!rootName || rootName === firstReadRootId()) return '';
  return `root=${encodeURIComponent(rootName)}`;
}

function appendRootQuery(url, rootName) {
  const q = rootQuery(rootName);
  if (!q) return url;
  return url + (url.includes('?') ? '&' : '?') + q;
}

function isReadableRoot(rootName) {
  if (!rootName) return true;
  return readRoots.some(root => root.id === rootName);
}

function firstReadRootId() {
  return readRoots[0]?.id || 'read';
}

function readRootById(rootId) {
  return readRoots.find(root => root.id === rootId) || readRoots[0];
}

function fileListIdForRoot(rootId) {
  return `read-root-file-list-${rootId}`;
}

function writeFileListIdForRoot(rootId) {
  return `write-root-file-list-${rootId}`;
}

function collapsedSetForRoot(rootId) {
  if (!collapsedReadDirsByRoot[rootId]) {
    collapsedReadDirsByRoot[rootId] = new Set();
  }
  return collapsedReadDirsByRoot[rootId];
}

function firstWriteRootId() {
  return writeRoots[0]?.id || 'write';
}

function writeRootById(rootId) {
  return writeRoots.find(root => root.id === rootId) || writeRoots[0];
}

function collapsedWriteSetForRoot(rootId) {
  if (!collapsedWriteDirsByRoot[rootId]) {
    collapsedWriteDirsByRoot[rootId] = new Set();
  }
  return collapsedWriteDirsByRoot[rootId];
}

function isHTMLFilePath(path) {
  return typeof path === 'string' && /\.html?$/i.test(path);
}

function rawFileURL(path, rootName, worktreeName) {
  let url = `/api/raw?path=${encodeURIComponent(path)}`;
  url = appendRootQuery(url, rootName);
  if (worktreeName) {
    url += `&worktree=${encodeURIComponent(worktreeName)}`;
  }
  return url;
}

// パスの簡易バリデーション（多層防御）
function isValidPath(path) {
  if (!path || typeof path !== 'string') return false;
  // 危険なパターンを拒否
  if (path.includes('\0')) return false;        // null バイトを含むパスは無効
  if (path.startsWith('/')) return false;       // 絶対パスは無効
  if (path.startsWith('./')) return false;      // ./ で始まる相対パスも無効
  if (path.includes('..')) return false;        // ディレクトリトラバーサルを防止
  return true;
}

// URL クエリパラメータを設定または削除
function setOrDeleteParam(url, key, value) {
  if (value) url.searchParams.set(key, value);
  else url.searchParams.delete(key);
}

// URLを更新（ブラウザ履歴に追加）。editFile はグローバル currentEditFile から取り込む
function updateURL(filePath, worktreeName) {
  const url = new URL(window.location);
  setOrDeleteParam(url, 'file', filePath);
  setOrDeleteParam(url, 'worktree', worktreeName);
  setOrDeleteParam(url, 'root', currentReadRoot !== firstReadRootId() ? currentReadRoot : '');
  setOrDeleteParam(url, 'editFile', currentEditFile);
  setOrDeleteParam(url, 'editRoot', currentEditRoot !== firstWriteRootId() ? currentEditRoot : '');
  history.pushState({ file: filePath, worktree: worktreeName, root: currentReadRoot, editFile: currentEditFile, editRoot: currentEditRoot }, '', url);
}

// URLを更新（履歴に追加しない、初期読み込み用）
function replaceURL(filePath, worktreeName) {
  const url = new URL(window.location);
  setOrDeleteParam(url, 'file', filePath);
  setOrDeleteParam(url, 'worktree', worktreeName);
  setOrDeleteParam(url, 'root', currentReadRoot !== firstReadRootId() ? currentReadRoot : '');
  setOrDeleteParam(url, 'editFile', currentEditFile);
  setOrDeleteParam(url, 'editRoot', currentEditRoot !== firstWriteRootId() ? currentEditRoot : '');
  history.replaceState({ file: filePath, worktree: worktreeName, root: currentReadRoot, editFile: currentEditFile, editRoot: currentEditRoot }, '', url);
}

// 編集ファイル選択時の URL 更新（read 側のパラメータは現在値を維持）
function updateEditURL(editFilePath) {
  const url = new URL(window.location);
  setOrDeleteParam(url, 'editFile', editFilePath);
  setOrDeleteParam(url, 'editRoot', currentEditRoot !== firstWriteRootId() ? currentEditRoot : '');
  history.pushState({ file: currentDocument, worktree: currentWorktree, root: currentReadRoot, editFile: editFilePath, editRoot: currentEditRoot }, '', url);
}

// 編集ファイル選択時の URL 置換（履歴に追加しない、初期読み込み用）
function replaceEditURL(editFilePath) {
  const url = new URL(window.location);
  setOrDeleteParam(url, 'editFile', editFilePath);
  setOrDeleteParam(url, 'editRoot', currentEditRoot !== firstWriteRootId() ? currentEditRoot : '');
  history.replaceState({ file: currentDocument, worktree: currentWorktree, root: currentReadRoot, editFile: editFilePath, editRoot: currentEditRoot }, '', url);
}

// 初期化
document.addEventListener('DOMContentLoaded', async () => {
  // サイドバーの永続化状態を最初に復元（renderFileList より前）
  restoreSidebarPersistedState();

  // サイドバー幅の復元とリサイザの初期化
  initSidebarResizer();
  initSidebarSectionResizer();

  // 閲覧/編集ペイン間リサイザの初期化（保存比率の復元含む）
  initPaneResizer();

  // ファイル絞り込み入力の初期化
  initFileFilter();

  // ドキュメント内の相対リンクを SPA ナビゲーションに委譲
  initDocLinkInterceptor();

  // 編集ペイン（textarea でのキー操作と保存ボタン）の初期化
  initEditPane();

  // 設定取得（-write の有効/無効など）
  try {
    await loadConfig();
  } catch (e) {
    console.warn('config の取得に失敗:', e);
  }
  applyWriteEnabledLayout();
  readRoots.forEach(root => loadCollapsedReadDirs(root.id));
  writeRoots.forEach(root => loadCollapsedWriteDirs(root.id));
  ensureReadSections();
  ensureWriteSections();

  for (const root of readRoots) {
    await loadWorktrees(root.id);
  }

  // ツリーを読み込み
  try {
    for (const root of readRoots) {
      await loadTree(root.id);
    }
    if (writeEnabled) {
      for (const root of writeRoots) {
        await loadWriteTree(root.id);
      }
    }
  } catch (error) {
    console.error('Failed to initialize tree or worktrees:', error);
    return;
  }

  // URLパラメータから初期状態を復元
  const params = getURLParams();
  if (params.root && isReadableRoot(params.root)) {
    currentReadRoot = params.root;
  } else {
    currentReadRoot = firstReadRootId();
  }

  // 現在の閲覧ルートの worktree 一覧を読み込み
  await loadWorktrees(currentReadRoot);

  // worktreeの設定（URLパラメータ優先）
  if (params.worktree && worktrees.some(wt => wt.name === params.worktree)) {
    currentWorktree = params.worktree;
  }

  // ファイルの自動選択
  if (params.file && isValidPath(params.file)) {
    await selectFileByPath(params.file);
  }

  // 編集ファイルの自動選択（writeEnabled 時のみ）
  if (writeEnabled && params.editFile && isValidPath(params.editFile)) {
    if (params.editRoot && writeRoots.some(root => root.id === params.editRoot)) {
      currentEditRoot = params.editRoot;
    }
    await selectEditFileByPath(params.editFile);
  }

  // ポーリング開始
  startPolling();
  startTreePolling();
});

// パスからファイルを選択（URL復元用）
async function selectFileByPath(path) {
  // ファイルリストから該当要素を探す
  const containerId = fileListIdForRoot(currentReadRoot);
  const fileItem = document.querySelector(`#${containerId} .file-item[data-path="${CSS.escape(path)}"]`);

  if (fileItem) {
    // URLを再度更新しないようにskipURLUpdate=true
    await selectFile(path, fileItem, true, currentReadRoot);
    // URLを正規化（replaceStateで履歴を上書き）
    replaceURL(path, currentWorktree);
  } else {
    console.warn('指定されたファイルがツリーに見つかりません:', path);
  }
}

// Worktree一覧読み込み
async function loadWorktrees(rootName = currentReadRoot) {
  try {
    const res = await fetch(appendRootQuery('/api/worktrees', rootName));
    const data = await res.json();
    const list = data.worktrees || [];
    worktreesByRoot[rootName] = list;
    if (rootName !== currentReadRoot) return;
    worktrees = list;

    // 現在のworktreeを設定
    const current = worktrees.find(wt => wt.current);
    if (current) {
      currentWorktree = current.name;
    }
  } catch (err) {
    console.error('worktree一覧の読み込みに失敗しました:', err);
    worktreesByRoot[rootName] = [];
    if (rootName === currentReadRoot) worktrees = [];
  }
}

// Worktree一覧をハッシュ付きで読み込み
async function loadWorktreesWithHashes(path, rootName = currentReadRoot) {
  try {
    let url = `/api/worktrees?path=${encodeURIComponent(path)}`;
    url = appendRootQuery(url, rootName);
    const res = await fetch(url);
    const data = await res.json();
    const list = data.worktrees || [];
    worktreesByRoot[rootName] = list;
    if (rootName !== currentReadRoot) return;
    worktrees = list;
    if (!worktrees.some(wt => wt.name === currentWorktree)) {
      const current = worktrees.find(wt => wt.current);
      currentWorktree = current ? current.name : null;
    }
  } catch (err) {
    console.error('worktree一覧の読み込みに失敗しました:', err);
  }
}

// Worktreeタブを描画（ハッシュ比較による色分け）
function renderWorktreeTabs() {
  const container = document.getElementById('worktree-tabs');

  // worktreeが1つ以下の場合はタブを表示しない
  if (worktrees.length <= 1) {
    container.classList.add('hidden');
    return;
  }

  container.innerHTML = '';
  container.classList.remove('hidden');

  // 現在のworktreeのハッシュを取得
  const currentWt = worktrees.find(wt => wt.name === currentWorktree);
  const currentHash = currentWt?.fileHash;

  worktrees.forEach(wt => {
    const tab = document.createElement('button');
    tab.className = 'worktree-tab';
    tab.textContent = wt.name;
    tab.dataset.worktree = wt.name;

    const isActive = wt.name === currentWorktree;
    const exists = wt.fileHash !== null && wt.fileHash !== undefined;
    const isDifferent = exists && currentHash && wt.fileHash !== currentHash;

    if (!exists) {
      // ファイル未存在: 薄いグレー + 打ち消し線
      tab.classList.add('not-exists');
    } else if (isActive) {
      // アクティブ: 青
      tab.classList.add('active');
    } else if (isDifferent) {
      // 内容が異なる: オレンジ
      tab.classList.add('different');
    }
    // 同じ内容: デフォルトスタイル

    tab.addEventListener('click', () => {
      selectWorktree(wt.name);
    });

    container.appendChild(tab);
  });
}

// Worktree選択
async function selectWorktree(name, skipURLUpdate = false) {
  if (name === currentWorktree) return;

  currentWorktree = name;

  isDiffMode = false;

  // URLを更新（popstate時はスキップ）
  if (!skipURLUpdate && currentDocument) {
    updateURL(currentDocument, currentWorktree);
  }

  // 現在のドキュメントを再読み込み
  if (currentDocument) {
    // ハッシュ付きでworktree一覧を再読み込み（選択worktree変更後の色分け更新）
    await loadWorktreesWithHashes(currentDocument);
    renderWorktreeTabs();

    // 差分データを読み込み
    await loadDiff(currentDocument);

    // 差分ボタンバーの表示/非表示を更新
    updateDiffButtonBar();

    // 差分表示の状態を更新
    updateDiffView();

    await loadFile(currentDocument);
  }
}

// 閲覧ツリー読み込み
async function loadTree(rootId = firstReadRootId()) {
  try {
    const res = await fetch(appendRootQuery('/api/tree', rootId));
    readTreeDataByRoot[rootId] = await res.json();
    renderAllReadTrees();
  } catch (err) {
    console.error('ファイルリストの読み込みに失敗しました:', err);
  }
}

// 編集ツリー読み込み
async function loadWriteTree(rootId = firstWriteRootId()) {
  try {
    const res = await fetch(`/api/tree?root=${encodeURIComponent(rootId)}`);
    writeTreeDataByRoot[rootId] = await res.json();
    renderAllWriteTrees();
  } catch (err) {
    console.error('編集ツリーの読み込みに失敗しました:', err);
  }
}

// サーバから設定取得
async function loadConfig() {
  const res = await fetch('/api/config');
  if (!res.ok) return;
  const cfg = await res.json();
  writeEnabled = !!cfg.writeEnabled;
  if (Array.isArray(cfg.readRoots) && cfg.readRoots.length > 0) {
    readRoots = cfg.readRoots.map((root, i) => ({
      id: root.id || (i === 0 ? 'read' : `read-${i + 1}`),
      name: root.name || '',
      sortDesc: !!root.sortDesc,
    }));
  } else {
    readRoots = [{ id: 'read', name: cfg.readRootName || '', sortDesc: !!cfg.readRootSortDesc }];
  }
  if (Array.isArray(cfg.writeRoots) && cfg.writeRoots.length > 0) {
    writeRoots = cfg.writeRoots.map((root, i) => ({
      id: root.id || (i === 0 ? 'write' : `write-${i + 1}`),
      name: root.name || '',
      sortDesc: !!root.sortDesc,
    }));
  } else if (writeEnabled) {
    writeRoots = [{ id: 'write', name: cfg.writeRootName || '', sortDesc: !!cfg.writeRootSortDesc }];
  } else {
    writeRoots = [];
  }
}

// 編集機能の有効/無効に応じてレイアウトを反映
function applyWriteEnabledLayout() {
  const writeSection = document.getElementById('sidebar-section-write');
  const writePane = document.getElementById('pane-write');
  const paneResizer = document.getElementById('pane-resizer');
  const mobileTabs = document.getElementById('mobile-pane-tabs');
  const sectionResizer = document.getElementById('sidebar-section-resizer');
  if (writeEnabled) {
    writeSection.classList.remove('hidden');
    writePane.classList.remove('hidden');
    if (paneResizer) paneResizer.classList.remove('hidden');
    if (mobileTabs) mobileTabs.classList.remove('hidden');
    setSidebarSectionHidden('write', lsGet(WRITE_SECTION_HIDDEN_KEY) === '1');
    restoreSidebarSplit();
  } else {
    writeSection.classList.add('hidden');
    writeSection.classList.remove('section-hidden');
    writePane.classList.add('hidden');
    if (paneResizer) paneResizer.classList.add('hidden');
    if (mobileTabs) mobileTabs.classList.add('hidden');
    // 編集ペインを隠したらサイズ指定もクリア（pane-read を 100% に戻す）
    const readPane = document.getElementById('pane-read');
    if (readPane) readPane.style.flex = '';
  }
  updateSidebarSectionResizerVisibility();
}

function ensureReadSections() {
  const firstSection = document.getElementById('sidebar-section-read');
  if (!firstSection) return;
  const title = document.getElementById('sidebar-section-read-title');
  if (title) title.textContent = 'readings';
  updateSidebarSectionToggle('read');
}

function ensureWriteSections() {
  const title = document.getElementById('sidebar-section-write-title');
  if (title) title.textContent = 'writings';
  updateSidebarSectionToggle('write');
}

function sidebarSectionByName(name) {
  return document.getElementById(name === 'write' ? 'sidebar-section-write' : 'sidebar-section-read');
}

function sidebarSectionHiddenKey(name) {
  return name === 'write' ? WRITE_SECTION_HIDDEN_KEY : READ_SECTION_HIDDEN_KEY;
}

function updateSidebarSectionToggle(name) {
  const section = sidebarSectionByName(name);
  const btn = document.getElementById(name === 'write' ? 'toggle-write-section-btn' : 'toggle-read-section-btn');
  if (!section || !btn) return;
  const hidden = section.classList.contains('section-hidden');
  btn.textContent = hidden ? '▸' : '▾';
  btn.title = hidden
    ? `${name === 'write' ? 'writings' : 'readings'}を表示`
    : `${name === 'write' ? 'writings' : 'readings'}を非表示`;
}

function updateSidebarSectionResizerVisibility() {
  const resizer = document.getElementById('sidebar-section-resizer');
  const readSection = document.getElementById('sidebar-section-read');
  const writeSection = document.getElementById('sidebar-section-write');
  if (!resizer || !readSection || !writeSection) return;

  const shouldHide = !writeEnabled ||
    writeSection.classList.contains('hidden') ||
    readSection.classList.contains('section-hidden') ||
    writeSection.classList.contains('section-hidden');
  resizer.classList.toggle('hidden', shouldHide);
}

function setSidebarSectionHidden(name, hidden) {
  const section = sidebarSectionByName(name);
  if (!section) return;
  section.classList.toggle('section-hidden', hidden);
  if (hidden) lsSet(sidebarSectionHiddenKey(name), '1');
  else lsRemove(sidebarSectionHiddenKey(name));
  updateSidebarSectionToggle(name);
  updateSidebarSectionResizerVisibility();
}

function toggleSidebarSection(name) {
  const section = sidebarSectionByName(name);
  if (!section) return;
  setSidebarSectionHidden(name, !section.classList.contains('section-hidden'));
}

// スマホ表示で表示するペインを切替（'read' | 'write'）。
// 切替制御クラスを content-wrapper に付与し、CSS @media のルールで表示を絞る。
function setMobilePane(pane) {
  const wrapper = document.querySelector('.content-wrapper');
  if (!wrapper) return;
  if (pane === 'write') {
    wrapper.classList.add('mobile-show-write');
  } else {
    wrapper.classList.remove('mobile-show-write');
  }
  document.querySelectorAll('.mobile-pane-tab').forEach(t => {
    t.classList.toggle('active', t.dataset.pane === pane);
  });
}

// ツリーをフラット化してルート相対のディレクトリごとにグループ化
function flattenTree(item) {
  const groups = {};

  function traverse(node, currentPath) {
    if (!node.isDir) {
      // ファイルの場合、親ディレクトリでグループ化。ルート直下は空文字キー。
      const dirPath = currentPath;
      if (!groups[dirPath]) {
        groups[dirPath] = [];
      }
      groups[dirPath].push({
        name: node.name,
        path: node.path,
        worktrees: node.worktrees || []
      });
    } else if (node.children && node.children.length > 0) {
      // ディレクトリの場合、子を再帰的に処理
      const newPath = currentPath ? currentPath + node.name + '/' : node.name + '/';
      node.children.forEach(child => {
        traverse(child, newPath);
      });
    }
  }

  // ルートの子から開始
  if (item.children) {
    item.children.forEach(child => {
      traverse(child, '');
    });
  }

  return groups;
}

function appendFileItems(container, files, keyword, worktreeCount, onSelectFile, opts = {}) {
  const htmlFilesOpenInNewTab = !!opts.htmlFilesOpenInNewTab;
  const rawLinkRoot = opts.rawLinkRoot || currentReadRoot;
  const rawLinkWorktree = opts.rawLinkWorktree ?? currentWorktree;

  files.forEach(file => {
    const isHTMLLink = htmlFilesOpenInNewTab && isHTMLFilePath(file.path);
    const fileItem = document.createElement(isHTMLLink ? 'a' : 'div');
    fileItem.className = 'file-item';
    fileItem.dataset.path = file.path;
    if (isHTMLLink) {
      fileItem.href = rawFileURL(file.path, rawLinkRoot, rawLinkWorktree);
      fileItem.target = '_blank';
      fileItem.rel = 'noopener noreferrer';
    }

    const nameSpan = document.createElement('span');
    nameSpan.className = 'file-name';
    if (keyword) {
      appendHighlightedText(nameSpan, file.name, keyword);
    } else {
      nameSpan.textContent = file.name;
    }
    fileItem.appendChild(nameSpan);

    if (file.worktrees.length > 0 && file.worktrees.length < worktreeCount) {
      const badgesContainer = document.createElement('span');
      badgesContainer.className = 'wt-badges';
      file.worktrees.forEach(wtName => {
        const badge = document.createElement('span');
        badge.className = 'wt-badge';
        badge.textContent = wtName;
        badgesContainer.appendChild(badge);
      });
      fileItem.appendChild(badgesContainer);
    }

    if (!isHTMLLink) {
      fileItem.addEventListener('click', () => {
        onSelectFile(file.path, fileItem);
      });
    }
    container.appendChild(fileItem);
  });
}

// ファイルリスト描画。
// opts:
//   collapsedSet  - 折り畳み状態を保持する Set（呼び出し側ごとに別物）
//   saveCollapsed - 折り畳み変更時の保存コールバック
//   onSelectFile  - ファイル行クリック時のハンドラ (path, fileItem) => void
//   applyDirFilter - true なら currentFilterDir を適用（閲覧ツリーのみ）
//   onCollapsedChange - 折り畳み状態変更後のコールバック（トグルボタン表示更新用）
function renderFileList(data, container, opts = {}) {
  if (!container) return;
  const collapsedSet = opts.collapsedSet || collapsedSetForRoot(firstReadRootId());
  const saveCollapsed = opts.saveCollapsed || (() => {});
  const onSelectFile = opts.onSelectFile || selectFile;
  const applyDirFilter = opts.applyDirFilter !== false; // デフォルト true
  const onCollapsedChange = opts.onCollapsedChange || (() => {});
  const sortDesc = !!opts.sortDesc;
  const worktreeCount = opts.worktreeCount ?? worktrees.length;
  const enableDirFilterLinks = opts.enableDirFilterLinks !== false;
  const appendFileOptions = {
    htmlFilesOpenInNewTab: !!opts.htmlFilesOpenInNewTab,
    rawLinkRoot: opts.rawLinkRoot || currentReadRoot,
    rawLinkWorktree: opts.rawLinkWorktree ?? currentWorktree,
  };

  container.innerHTML = '';
  const groups = flattenTree(data);

  // ディレクトリパスでソート
  const sortedDirs = Object.keys(groups).sort((a, b) => sortDesc ? b.localeCompare(a) : a.localeCompare(b));

  // フィルター時は戻るバーを表示（閲覧ツリーのみ）
  if (applyDirFilter && currentFilterDir) {
    const backBar = document.createElement('div');
    backBar.className = 'dir-filter-bar';

    const backBtn = document.createElement('button');
    backBtn.className = 'dir-filter-back-btn';
    backBtn.textContent = '← 戻る';
    backBtn.addEventListener('click', () => exitDirFilter());

    const breadcrumb = document.createElement('span');
    breadcrumb.className = 'dir-filter-breadcrumb';

    // パスをセグメントに分割してそれぞれクリック可能にする
    const displayPath = currentFilterDir.replace(/^\.\//, '');
    const prefix = currentFilterDir.startsWith('./') ? './' : '';
    const segments = displayPath.replace(/\/$/, '').split('/');

    segments.forEach((seg, i) => {
      if (i > 0) {
        const sep = document.createElement('span');
        sep.className = 'dir-filter-sep';
        sep.textContent = '/';
        breadcrumb.appendChild(sep);
      }

      const segPath = prefix + segments.slice(0, i + 1).join('/') + '/';
      const link = document.createElement('span');
      link.className = 'dir-filter-segment';
      link.textContent = seg;

      // 最後のセグメント以外はクリックで上位ディレクトリにフィルター
      if (i < segments.length - 1) {
        link.classList.add('dir-filter-segment-link');
        link.addEventListener('click', () => enterDirFilter(segPath));
      } else {
        link.classList.add('dir-filter-segment-current');
      }

      breadcrumb.appendChild(link);
    });

    backBar.appendChild(backBtn);
    backBar.appendChild(breadcrumb);
    container.appendChild(backBar);
  }

  const keyword = currentFilterKeyword.toLowerCase();
  let renderedFileCount = 0;

  sortedDirs.forEach(dirPath => {
    // ディレクトリフィルター適用（閲覧ツリーのみ）
    if (applyDirFilter && currentFilterDir && !dirPath.startsWith(currentFilterDir)) {
      return;
    }

    let files = groups[dirPath];

    // キーワードフィルター適用（ファイル名 or パスに対する部分一致、大文字小文字区別なし）
    if (keyword) {
      files = files.filter(f =>
        f.name.toLowerCase().includes(keyword) ||
        f.path.toLowerCase().includes(keyword)
      );
      if (files.length === 0) return;
    }

    renderedFileCount += files.length;

    files = files.slice().sort((a, b) => sortDesc ? b.name.localeCompare(a.name) : a.name.localeCompare(b.name));
    const totalWorktreeCount = worktreeCount;

    if (dirPath === '') {
      appendFileItems(container, files, keyword, totalWorktreeCount, onSelectFile, appendFileOptions);
      return;
    }

    // ディレクトリグループ
    const groupDiv = document.createElement('div');
    groupDiv.className = 'dir-group';
    // キーワード絞り込み中は折り畳みを無効化（ヒット結果は常に展開）
    if (!keyword && collapsedSet.has(dirPath)) {
      groupDiv.classList.add('collapsed');
    }

    const label = document.createElement('div');
    label.className = 'dir-label';

    const toggle = document.createElement('span');
    toggle.className = 'dir-label-toggle';
    toggle.textContent = '▶';
    toggle.addEventListener('click', (e) => {
      e.stopPropagation();
      if (collapsedSet.has(dirPath)) {
        collapsedSet.delete(dirPath);
        groupDiv.classList.remove('collapsed');
      } else {
        collapsedSet.add(dirPath);
        groupDiv.classList.add('collapsed');
      }
      saveCollapsed();
      onCollapsedChange();
    });
    label.appendChild(toggle);

    const displayPath = dirPath.replace(/^\.\//, '');
    const prefix = dirPath.startsWith('./') ? './' : '';
    const segments = displayPath.replace(/\/$/, '').split('/');

    segments.forEach((seg, i) => {
      if (i > 0) {
        const sep = document.createElement('span');
        sep.className = 'dir-label-sep';
        sep.textContent = '/';
        label.appendChild(sep);
      }

      const segPath = prefix + segments.slice(0, i + 1).join('/') + '/';
      const link = document.createElement('span');
      link.className = 'dir-label-segment';
      link.textContent = seg;
      if (enableDirFilterLinks) {
        link.addEventListener('click', (e) => {
          e.stopPropagation();
          enterDirFilter(segPath);
        });
      }
      label.appendChild(link);
    });

    const trailingSlash = document.createElement('span');
    trailingSlash.className = 'dir-label-sep';
    trailingSlash.textContent = '/';
    label.appendChild(trailingSlash);

    groupDiv.appendChild(label);

    // ファイル一覧
    appendFileItems(groupDiv, files, keyword, totalWorktreeCount, onSelectFile, appendFileOptions);

    container.appendChild(groupDiv);
  });

  // キーワードヒット 0 件時のメッセージ
  if (keyword && renderedFileCount === 0) {
    const empty = document.createElement('div');
    empty.className = 'file-filter-empty';
    empty.textContent = `「${currentFilterKeyword}」に一致するファイルはありません`;
    container.appendChild(empty);
  }
}

function renderRootAccordion(container, root, opts = {}) {
  const group = document.createElement('div');
  group.className = `root-accordion root-accordion-${opts.type || 'read'}`;
  group.dataset.root = root.id;
  if (opts.selected) group.classList.add('active');

  const header = document.createElement('div');
  header.className = 'root-accordion-header';

  const toggle = document.createElement('span');
  toggle.className = 'root-accordion-toggle';
  toggle.textContent = '▶';
  header.appendChild(toggle);

  const label = document.createElement('span');
  label.className = 'root-accordion-label';
  label.textContent = root.name || root.id;
  header.appendChild(label);

  const body = document.createElement('div');
  body.className = 'root-accordion-body';
  body.id = opts.type === 'write' ? writeFileListIdForRoot(root.id) : fileListIdForRoot(root.id);

  const collapsedSet = opts.collapsedSet;
  const groupKey = `root:${root.id}`;
  if (collapsedSet && collapsedSet.has(groupKey)) {
    group.classList.add('collapsed');
  }

  header.addEventListener('click', () => {
    group.classList.toggle('collapsed');
    if (collapsedSet) {
      if (group.classList.contains('collapsed')) {
        collapsedSet.add(groupKey);
      } else {
        collapsedSet.delete(groupKey);
      }
    }
    if (opts.type === 'write') saveCollapsedWriteDirs(root.id);
    else saveCollapsedReadDirs(root.id);
  });

  group.appendChild(header);
  group.appendChild(body);
  container.appendChild(group);
  return body;
}

// 部分一致した範囲を <mark> でハイライトしながらテキストを追加（XSS回避のため textContent ベース）
function appendHighlightedText(parent, text, keyword) {
  const lower = text.toLowerCase();
  const klen = keyword.length;
  let idx = 0;
  while (idx < text.length) {
    const hit = lower.indexOf(keyword, idx);
    if (hit === -1) {
      parent.appendChild(document.createTextNode(text.slice(idx)));
      return;
    }
    if (hit > idx) {
      parent.appendChild(document.createTextNode(text.slice(idx, hit)));
    }
    const mark = document.createElement('span');
    mark.className = 'file-filter-highlight';
    mark.textContent = text.slice(hit, hit + klen);
    parent.appendChild(mark);
    idx = hit + klen;
  }
}

// ドキュメント内の `?file=...&worktree=...` 形式リンクをクリックで SPA ナビへ委譲する。
// goldmark レンダ時にサーバ側で書き換えた相対リンクがこの形式になっている。
function initDocLinkInterceptor() {
  const docEl = document.getElementById('document');
  if (!docEl) return;

  docEl.addEventListener('click', async (e) => {
    // 修飾キー押下時はブラウザ既定動作（新しいタブで開く等）に任せる
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;

    const link = e.target.closest('a');
    if (!link) return;

    // クエリ文字列だけのリンクのみ対象（例: ?file=docs/x.md&worktree=main）
    const href = link.getAttribute('href') || '';
    if (!href.startsWith('?')) return;

    const params = new URLSearchParams(href.slice(1));
    const filePath = params.get('file');
    if (!filePath || !isValidPath(filePath)) return;

    e.preventDefault();

    const worktreeName = params.get('worktree');
    if (worktreeName && worktrees.some(wt => wt.name === worktreeName) && worktreeName !== currentWorktree) {
      await selectWorktree(worktreeName, true);
    }

    await selectFileByPath(filePath);

    // フラグメントがあればスクロール
    const hashIdx = href.indexOf('#');
    if (hashIdx >= 0) {
      const target = document.getElementById(href.slice(hashIdx + 1));
      if (target) target.scrollIntoView();
    }
  });
}

async function copyTextToClipboard(text) {
  if (!text) throw new Error('copy text is empty');

  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text);
    return;
  }

  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', '');
  textarea.style.position = 'fixed';
  textarea.style.top = '-1000px';
  textarea.style.left = '-1000px';
  document.body.appendChild(textarea);
  textarea.select();
  try {
    if (!document.execCommand('copy')) {
      throw new Error('execCommand copy failed');
    }
  } finally {
    document.body.removeChild(textarea);
  }
}

function showCopyButtonStatus(btn, message, isError = false) {
  if (!btn) return;
  const original = btn.dataset.defaultText || btn.textContent;
  btn.dataset.defaultText = original;
  btn.textContent = message;
  btn.classList.toggle('copied', !isError);
  clearTimeout(showCopyButtonStatus._timers?.[btn.id]);
  showCopyButtonStatus._timers = showCopyButtonStatus._timers || {};
  showCopyButtonStatus._timers[btn.id] = setTimeout(() => {
    btn.textContent = original;
    btn.classList.remove('copied');
  }, 1500);
}

async function copyReadFilename() {
  if (!currentDocument) return;
  const btn = document.getElementById('read-copy-btn');
  const filename = document.getElementById('read-filename')?.textContent || currentDocument;
  try {
    await copyTextToClipboard(filename);
    showCopyButtonStatus(btn, 'コピーしました');
  } catch (e) {
    console.error('ファイル名のコピーに失敗しました:', e);
    showCopyButtonStatus(btn, 'コピー失敗', true);
  }
}

async function copyWriteFilename() {
  if (!currentEditFile) return;
  const btn = document.getElementById('edit-copy-btn');
  const filename = document.getElementById('edit-filename')?.textContent || currentEditFile;
  try {
    await copyTextToClipboard(filename);
    showCopyButtonStatus(btn, 'コピーしました');
  } catch (e) {
    console.error('ファイル名のコピーに失敗しました:', e);
    showCopyButtonStatus(btn, 'コピー失敗', true);
  }
}

// ファイル絞り込み入力の初期化
function initFileFilter() {
  const input = document.getElementById('file-filter-input');
  const clearBtn = document.getElementById('file-filter-clear');
  if (!input || !clearBtn) return;

  const apply = () => {
    currentFilterKeyword = input.value.trim();
    if (currentFilterKeyword) {
      clearBtn.classList.remove('hidden');
      lsSet(KEYWORD_FILTER_KEY, currentFilterKeyword);
    } else {
      clearBtn.classList.add('hidden');
      lsRemove(KEYWORD_FILTER_KEY);
    }
    renderAllReadTrees();
    renderAllWriteTrees();
  };

  input.addEventListener('input', apply);
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      input.value = '';
      apply();
      input.blur();
    }
  });
  clearBtn.addEventListener('click', () => {
    input.value = '';
    apply();
    input.focus();
  });
}

// ディレクトリフィルター開始
function enterDirFilter(dirPath) {
  currentFilterDir = dirPath;
  lsSet(DIR_FILTER_KEY, dirPath);
  renderAllReadTrees();
}

// ディレクトリフィルター解除
function exitDirFilter() {
  currentFilterDir = null;
  lsRemove(DIR_FILTER_KEY);
  renderAllReadTrees();
}

function renderAllReadTrees() {
  const container = document.getElementById('file-list');
  if (!container) return;
  container.innerHTML = '';
  readRoots.forEach(root => renderReadTree(root.id, container));
}

// 閲覧ツリー描画（描画後に選択状態を復元）
function renderReadTree(rootId = firstReadRootId(), parentContainer = null) {
  const root = readRootById(rootId);
  const data = readTreeDataByRoot[root.id];
  if (!root || !data) return;
  const container = parentContainer || document.getElementById('file-list');
  const body = renderRootAccordion(container, root, {
    type: 'read',
    selected: currentReadRoot === root.id,
    collapsedSet: collapsedSetForRoot(root.id),
  });
  renderFileList(data, body, {
    collapsedSet: collapsedSetForRoot(root.id),
    saveCollapsed: () => saveCollapsedReadDirs(root.id),
    onSelectFile: (path, fileItem) => selectFile(path, fileItem, false, root.id),
    applyDirFilter: root.id === firstReadRootId(),
    enableDirFilterLinks: root.id === firstReadRootId(),
    onCollapsedChange: () => updateToggleAllReadButton(root.id),
    sortDesc: root.sortDesc,
    worktreeCount: (worktreesByRoot[root.id] || []).length,
    htmlFilesOpenInNewTab: true,
    rawLinkRoot: root.id,
    rawLinkWorktree: root.id === currentReadRoot ? currentWorktree : null,
  });
  if (currentReadRoot === root.id && currentDocument) restoreSelectionInElement(body, currentDocument);
  updateToggleAllReadButton(root.id);
}

// 編集ツリー描画
function renderAllWriteTrees() {
  const container = document.getElementById('write-file-list');
  if (!container) return;
  container.innerHTML = '';
  writeRoots.forEach(root => renderWriteTree(root.id, container));
}

function renderWriteTree(rootId = firstWriteRootId(), parentContainer = null) {
  const root = writeRootById(rootId);
  const data = root && writeTreeDataByRoot[root.id];
  if (!writeEnabled || !root || !data) return;
  const container = parentContainer || document.getElementById('write-file-list');
  const body = renderRootAccordion(container, root, {
    type: 'write',
    selected: currentEditRoot === root.id,
    collapsedSet: collapsedWriteSetForRoot(root.id),
  });
  renderFileList(data, body, {
    collapsedSet: collapsedWriteSetForRoot(root.id),
    saveCollapsed: () => saveCollapsedWriteDirs(root.id),
    onSelectFile: (path, fileItem) => selectEditFile(path, fileItem, false, root.id),
    applyDirFilter: false,
    enableDirFilterLinks: false,
    onCollapsedChange: () => updateToggleAllWriteButton(root.id),
    sortDesc: root.sortDesc,
  });
  if (currentEditRoot === root.id && currentEditFile) restoreSelectionInElement(body, currentEditFile);
  updateToggleAllWriteButton(root.id);
}

// ===== 全展開・全折り畳みボタン =====

// 指定ツリーデータに含まれる全ディレクトリパスを返す（flattenTree のキー集合）
function allDirPathsIn(treeDataObj) {
  if (!treeDataObj) return [];
  const groups = flattenTree(treeDataObj);
  return Object.keys(groups).filter(p => p !== '');
}

function allCollapseKeysForRoot(rootId, treeDataObj) {
  if (!treeDataObj) return [];
  return [`root:${rootId}`, ...allDirPathsIn(treeDataObj)];
}

function allCollapseKeysCollapsed(keys, collapsedSet) {
  return keys.length > 0 && keys.every(p => collapsedSet.has(p));
}

function updateSectionToggleButton(btnId, roots, treeDataByRoot, collapsedSetProvider) {
  const btn = document.getElementById(btnId);
  if (!btn) return;

  let totalKeys = 0;
  let allCollapsed = true;
  roots.forEach(root => {
    const keys = allCollapseKeysForRoot(root.id, treeDataByRoot[root.id]);
    if (keys.length === 0) return;
    totalKeys += keys.length;
    const collapsedSet = collapsedSetProvider(root.id);
    if (!allCollapseKeysCollapsed(keys, collapsedSet)) {
      allCollapsed = false;
    }
  });

  if (totalKeys === 0) {
    btn.disabled = true;
    return;
  }
  btn.disabled = false;
  if (allCollapsed) {
    btn.textContent = '⊞';
    btn.title = '全て展開';
  } else {
    btn.textContent = '⊟';
    btn.title = '全て折り畳み';
  }
}

function updateToggleAllReadButton() {
  updateSectionToggleButton('toggle-all-read-btn', readRoots, readTreeDataByRoot, collapsedSetForRoot);
}

function updateToggleAllWriteButton() {
  updateSectionToggleButton('toggle-all-write-btn', writeRoots, writeTreeDataByRoot, collapsedWriteSetForRoot);
}

// 閲覧ツリー: 全展開・全折り畳みのトグル
function toggleAllRead() {
  const rootsWithData = readRoots.filter(root => readTreeDataByRoot[root.id]);
  const shouldExpand = rootsWithData.length > 0 && rootsWithData.every(root => {
    const keys = allCollapseKeysForRoot(root.id, readTreeDataByRoot[root.id]);
    return allCollapseKeysCollapsed(keys, collapsedSetForRoot(root.id));
  });

  rootsWithData.forEach(root => {
    const data = readTreeDataByRoot[root.id];
    const collapsedSet = collapsedSetForRoot(root.id);
    const keys = allCollapseKeysForRoot(root.id, data);
    if (shouldExpand) {
      keys.forEach(p => collapsedSet.delete(p));
    } else {
      keys.forEach(p => collapsedSet.add(p));
    }
    saveCollapsedReadDirs(root.id);
  });
  renderAllReadTrees();
}

// 編集ツリー: 全展開・全折り畳みのトグル
function toggleAllWrite() {
  const rootsWithData = writeRoots.filter(root => writeTreeDataByRoot[root.id]);
  const shouldExpand = rootsWithData.length > 0 && rootsWithData.every(root => {
    const keys = allCollapseKeysForRoot(root.id, writeTreeDataByRoot[root.id]);
    return allCollapseKeysCollapsed(keys, collapsedWriteSetForRoot(root.id));
  });

  rootsWithData.forEach(root => {
    const data = writeTreeDataByRoot[root.id];
    const collapsedSet = collapsedWriteSetForRoot(root.id);
    const keys = allCollapseKeysForRoot(root.id, data);
    if (shouldExpand) {
      keys.forEach(p => collapsedSet.delete(p));
    } else {
      keys.forEach(p => collapsedSet.add(p));
    }
    saveCollapsedWriteDirs(root.id);
  });
  renderAllWriteTrees();
}

// 閲覧ツリーのファイル選択（左ペインに表示）
async function selectFile(path, rowElement, skipURLUpdate = false, rootName = firstReadRootId()) {
  currentReadRoot = rootName;
  // 閲覧ツリー内の以前の選択を解除（編集ツリー側はそのまま）
  document.querySelectorAll('.sidebar-section-read .file-item.selected').forEach(el => {
    el.classList.remove('selected');
  });

  if (rowElement) rowElement.classList.add('selected');

  isDiffMode = false;

  if (!skipURLUpdate) {
    updateURL(path, currentWorktree);
    // ユーザー操作起点ならスマホ表示で閲覧ペインに切替
    setMobilePane('read');
  }

  await loadWorktreesWithHashes(path, currentReadRoot);
  renderWorktreeTabs();
  await loadDiff(path);
  updateDiffButtonBar();
  updateDiffView();
  await loadFile(path);
}

// ファイル読み込み
async function loadFile(path) {
  try {
    // worktreeパラメータを追加
    let url = `/api/file?path=${encodeURIComponent(path)}`;
    url = appendRootQuery(url, currentReadRoot);
    if (currentWorktree) {
      url += `&worktree=${encodeURIComponent(currentWorktree)}`;
    }

    const res = await fetch(url);

    if (!res.ok) {
      if (res.status === 404) {
        // ファイルが見つからない場合
        document.getElementById('empty-state').classList.add('hidden');
        const docEl = document.getElementById('document');
        docEl.classList.remove('hidden');
        docEl.innerHTML = '';
        const message = document.createElement('div');
        message.className = 'not-found-message';
        message.textContent = `このファイルは ${currentWorktree || '選択中の worktree'} には存在しません`;
        docEl.appendChild(message);
        currentDocument = path;
        updateReadHeader(path);
        return;
      }
      throw new Error('ファイルの読み込みに失敗しました');
    }

    const data = await res.json();

    // 状態更新
    currentDocument = path;
    lastModifiedAt = data.modifiedAt;
    updateReadHeader(path);

    // 表示切り替え
    document.getElementById('empty-state').classList.add('hidden');
    const docEl = document.getElementById('document');
    docEl.classList.remove('hidden');
    docEl.innerHTML = data.html;
    updateReadHeader(path);

  } catch (err) {
    console.error('ファイルの読み込みに失敗しました:', err);
  }
}

// ポーリング開始（2秒間隔）
function startPolling() {
  setInterval(async () => {
    if (!currentDocument) return;

    try {
      let url = `/api/file?path=${encodeURIComponent(currentDocument)}`;
      url = appendRootQuery(url, currentReadRoot);
      if (currentWorktree) {
        url += `&worktree=${encodeURIComponent(currentWorktree)}`;
      }

      const res = await fetch(url);

      if (!res.ok) return;

      const data = await res.json();

      // 更新があれば再描画
      if (lastModifiedAt && data.modifiedAt !== lastModifiedAt) {
        const docEl = document.getElementById('document');
        const scrollTop = docEl.scrollTop;

        docEl.innerHTML = data.html;

        // スクロール位置を復元
        docEl.scrollTop = scrollTop;
      }

      lastModifiedAt = data.modifiedAt;

    } catch (err) {
      // ポーリングエラーは無視
    }
  }, 2000);
}

// ツリーのポーリング開始（5秒間隔、両ツリーを同時に更新）
function startTreePolling() {
  setInterval(async () => {
    for (const root of readRoots) {
      try {
        await loadTree(root.id);
      } catch (err) {
        // 閲覧ツリーのエラーは無視
      }
    }
    if (writeEnabled) {
      for (const root of writeRoots) {
        try {
          await loadWriteTree(root.id);
        } catch (err) {
          // 編集ツリーのエラーは無視
        }
      }
    }
  }, 5000);
}

// 選択状態を復元（指定コンテナ内のみハイライト）
function restoreSelectionIn(containerId, path) {
  const container = document.getElementById(containerId);
  if (!container) return;
  restoreSelectionInElement(container, path);
}

function restoreSelectionInElement(container, path) {
  container.querySelectorAll('.file-item').forEach(el => {
    if (el.dataset.path === path) {
      el.classList.add('selected');
    }
  });
}

// 互換性: 既存呼び出し用に restoreSelection を残す
function restoreSelection(path) {
  restoreSelectionIn('file-list', path);
}

// サイドバー状態の localStorage キー
const SIDEBAR_WIDTH_KEY = 'reader.sidebarWidth';
const PANE_READ_RATIO_KEY = 'reader.paneReadRatio';
const PANE_MIN_WIDTH = 200;
const SIDEBAR_COLLAPSED_KEY = 'reader.sidebarCollapsed';
const READ_SECTION_HIDDEN_KEY = 'reader.readSectionHidden';
const WRITE_SECTION_HIDDEN_KEY = 'reader.writeSectionHidden';
const SIDEBAR_SPLIT_RATIO_KEY = 'reader.sidebarSplitRatio';
const COLLAPSED_DIRS_KEY = 'reader.collapsedDirs';
const COLLAPSED_WRITE_DIRS_KEY = 'reader.collapsedWriteDirs';
const DIR_FILTER_KEY = 'reader.dirFilter';
const KEYWORD_FILTER_KEY = 'reader.keywordFilter';
const SIDEBAR_MIN_WIDTH = 120;
const SIDEBAR_MAX_WIDTH = 800;
const SIDEBAR_SECTION_MIN_HEIGHT = 80;

// localStorage のラッパ（容量オーバーや無効化時に落ちないように try で包む）
function lsGet(key) {
  try { return localStorage.getItem(key); } catch (e) { return null; }
}
function lsSet(key, value) {
  try { localStorage.setItem(key, value); } catch (e) { /* ignore */ }
}
function lsRemove(key) {
  try { localStorage.removeItem(key); } catch (e) { /* ignore */ }
}

// 折り畳み済みディレクトリの永続化
function collapsedStorageKey(rootId) {
  return rootId === firstReadRootId() ? COLLAPSED_DIRS_KEY : `${COLLAPSED_DIRS_KEY}.${rootId}`;
}
function saveCollapsedReadDirs(rootId = firstReadRootId()) {
  lsSet(collapsedStorageKey(rootId), JSON.stringify([...collapsedSetForRoot(rootId)]));
}
function loadCollapsedReadDirs(rootId = firstReadRootId()) {
  const collapsedSet = collapsedSetForRoot(rootId);
  const raw = lsGet(collapsedStorageKey(rootId));
  if (!raw) return;
  try {
    const arr = JSON.parse(raw);
    if (Array.isArray(arr)) {
      arr.forEach(p => { if (typeof p === 'string') collapsedSet.add(p); });
    }
  } catch (e) { /* ignore */ }
}

// 永続化された全サイドバー状態を起動時に復元する
// （ツリー描画前にグローバルへ反映しておけば renderFileList が自動で反映する）
function restoreSidebarPersistedState() {
  loadCollapsedReadDirs(firstReadRootId());
  loadCollapsedWriteDirs(firstWriteRootId());

  const dirFilter = lsGet(DIR_FILTER_KEY);
  if (dirFilter && isValidPath(dirFilter.replace(/\/$/, ''))) {
    currentFilterDir = dirFilter;
  }

  const keyword = lsGet(KEYWORD_FILTER_KEY);
  if (keyword) {
    currentFilterKeyword = keyword;
    const input = document.getElementById('file-filter-input');
    const clearBtn = document.getElementById('file-filter-clear');
    if (input) input.value = keyword;
    if (clearBtn) clearBtn.classList.remove('hidden');
  }

  if (lsGet(SIDEBAR_COLLAPSED_KEY) === '1') {
    const sidebar = document.getElementById('sidebar');
    const openBtn = document.getElementById('sidebar-open-btn');
    if (sidebar) sidebar.classList.add('collapsed');
    if (openBtn) openBtn.classList.remove('hidden');
  }

  setSidebarSectionHidden('read', lsGet(READ_SECTION_HIDDEN_KEY) === '1');
  setSidebarSectionHidden('write', lsGet(WRITE_SECTION_HIDDEN_KEY) === '1');
}

function initSidebarResizer() {
  const sidebar = document.getElementById('sidebar');
  const resizer = document.getElementById('sidebar-resizer');
  if (!sidebar || !resizer) return;

  // 保存済みの幅を復元
  const saved = parseInt(localStorage.getItem(SIDEBAR_WIDTH_KEY), 10);
  if (Number.isFinite(saved) && saved >= SIDEBAR_MIN_WIDTH && saved <= SIDEBAR_MAX_WIDTH) {
    sidebar.style.width = saved + 'px';
  }

  let startX = 0;
  let startWidth = 0;

  const onMouseMove = (e) => {
    const delta = e.clientX - startX;
    let newWidth = startWidth + delta;
    if (newWidth < SIDEBAR_MIN_WIDTH) newWidth = SIDEBAR_MIN_WIDTH;
    if (newWidth > SIDEBAR_MAX_WIDTH) newWidth = SIDEBAR_MAX_WIDTH;
    sidebar.style.width = newWidth + 'px';
  };

  const onMouseUp = () => {
    document.removeEventListener('mousemove', onMouseMove);
    document.removeEventListener('mouseup', onMouseUp);
    resizer.classList.remove('resizing');
    document.body.classList.remove('resizing-sidebar');
    const finalWidth = parseInt(sidebar.style.width, 10);
    if (Number.isFinite(finalWidth)) {
      localStorage.setItem(SIDEBAR_WIDTH_KEY, String(finalWidth));
    }
  };

  resizer.addEventListener('mousedown', (e) => {
    if (sidebar.classList.contains('collapsed')) return;
    e.preventDefault();
    startX = e.clientX;
    startWidth = sidebar.getBoundingClientRect().width;
    resizer.classList.add('resizing');
    document.body.classList.add('resizing-sidebar');
    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mouseup', onMouseUp);
  });

  // ダブルクリックでデフォルト幅にリセット
  resizer.addEventListener('dblclick', () => {
    sidebar.style.width = '';
    localStorage.removeItem(SIDEBAR_WIDTH_KEY);
  });
}

// 閲覧/編集ペイン間リサイザの初期化。
// pane-read に flex-basis を比率（0.0–1.0）で設定し、pane-write は flex:1 で残りを埋める。
// ウィンドウ幅が変わっても比率を保つため、保存値は比率（px ではない）。
function initPaneResizer() {
  const wrapper = document.querySelector('.content-wrapper');
  const readPane = document.getElementById('pane-read');
  const resizer = document.getElementById('pane-resizer');
  if (!wrapper || !readPane || !resizer) return;

  const applyRatio = (ratio) => {
    if (!Number.isFinite(ratio)) return;
    if (ratio < 0.05) ratio = 0.05;
    if (ratio > 0.95) ratio = 0.95;
    readPane.style.flex = `0 0 ${(ratio * 100).toFixed(4)}%`;
  };

  // 保存済みの比率を復元
  const saved = parseFloat(localStorage.getItem(PANE_READ_RATIO_KEY));
  if (Number.isFinite(saved) && saved > 0 && saved < 1) {
    applyRatio(saved);
  }

  let dragging = false;

  const onMouseMove = (e) => {
    if (!dragging) return;
    const rect = wrapper.getBoundingClientRect();
    if (rect.width <= 0) return;
    let newReadWidth = e.clientX - rect.left;
    if (newReadWidth < PANE_MIN_WIDTH) newReadWidth = PANE_MIN_WIDTH;
    if (newReadWidth > rect.width - PANE_MIN_WIDTH) newReadWidth = rect.width - PANE_MIN_WIDTH;
    applyRatio(newReadWidth / rect.width);
  };

  const onMouseUp = () => {
    if (!dragging) return;
    dragging = false;
    document.removeEventListener('mousemove', onMouseMove);
    document.removeEventListener('mouseup', onMouseUp);
    resizer.classList.remove('resizing');
    document.body.classList.remove('resizing-pane');
    // 現在の比率を保存
    const rect = wrapper.getBoundingClientRect();
    if (rect.width > 0) {
      const readRect = readPane.getBoundingClientRect();
      const ratio = readRect.width / rect.width;
      if (Number.isFinite(ratio)) {
        localStorage.setItem(PANE_READ_RATIO_KEY, ratio.toFixed(4));
      }
    }
  };

  resizer.addEventListener('mousedown', (e) => {
    if (resizer.classList.contains('hidden')) return;
    e.preventDefault();
    dragging = true;
    resizer.classList.add('resizing');
    document.body.classList.add('resizing-pane');
    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mouseup', onMouseUp);
  });

  // ダブルクリックでデフォルト（50/50）にリセット
  resizer.addEventListener('dblclick', () => {
    readPane.style.flex = '';
    localStorage.removeItem(PANE_READ_RATIO_KEY);
  });
}

function getSidebarSplitAvailableHeight() {
  const readSection = document.getElementById('sidebar-section-read');
  const writeSection = document.getElementById('sidebar-section-write');
  if (!readSection || !writeSection || writeSection.classList.contains('hidden') || readSection.classList.contains('section-hidden') || writeSection.classList.contains('section-hidden')) return 0;
  return readSection.getBoundingClientRect().height + writeSection.getBoundingClientRect().height;
}

function clampSidebarSplitHeight(height, availableHeight) {
  if (availableHeight <= SIDEBAR_SECTION_MIN_HEIGHT * 2) {
    return availableHeight / 2;
  }
  const maxHeight = availableHeight - SIDEBAR_SECTION_MIN_HEIGHT;
  if (height < SIDEBAR_SECTION_MIN_HEIGHT) return SIDEBAR_SECTION_MIN_HEIGHT;
  if (height > maxHeight) return maxHeight;
  return height;
}

function setSidebarSplitHeight(readHeight, shouldPersist) {
  const readSection = document.getElementById('sidebar-section-read');
  const writeSection = document.getElementById('sidebar-section-write');
  if (!readSection || !writeSection || writeSection.classList.contains('hidden')) return;

  const availableHeight = getSidebarSplitAvailableHeight();
  if (!Number.isFinite(availableHeight) || availableHeight <= 0) return;

  const nextHeight = clampSidebarSplitHeight(readHeight, availableHeight);
  readSection.style.flex = `0 0 ${nextHeight}px`;
  writeSection.style.flex = '1 1 0';

  if (shouldPersist) {
    lsSet(SIDEBAR_SPLIT_RATIO_KEY, String(nextHeight / availableHeight));
  }
}

function restoreSidebarSplit() {
  const ratio = parseFloat(lsGet(SIDEBAR_SPLIT_RATIO_KEY));
  if (!Number.isFinite(ratio) || ratio <= 0 || ratio >= 1) return;

  const availableHeight = getSidebarSplitAvailableHeight();
  if (availableHeight > 0) {
    setSidebarSplitHeight(availableHeight * ratio, false);
  }
}

function resetSidebarSplit() {
  const readSection = document.getElementById('sidebar-section-read');
  const writeSection = document.getElementById('sidebar-section-write');
  if (readSection) readSection.style.flex = '';
  if (writeSection) writeSection.style.flex = '';
  lsRemove(SIDEBAR_SPLIT_RATIO_KEY);
}

function initSidebarSectionResizer() {
  const resizer = document.getElementById('sidebar-section-resizer');
  const readSection = document.getElementById('sidebar-section-read');
  const writeSection = document.getElementById('sidebar-section-write');
  if (!resizer || !readSection || !writeSection) return;

  let startY = 0;
  let startHeight = 0;

  const onMouseMove = (e) => {
    const delta = e.clientY - startY;
    setSidebarSplitHeight(startHeight + delta, false);
  };

  const onMouseUp = () => {
    document.removeEventListener('mousemove', onMouseMove);
    document.removeEventListener('mouseup', onMouseUp);
    resizer.classList.remove('resizing');
    document.body.classList.remove('resizing-sidebar-section');

    const availableHeight = getSidebarSplitAvailableHeight();
    const finalHeight = readSection.getBoundingClientRect().height;
    if (availableHeight > 0 && Number.isFinite(finalHeight)) {
      lsSet(SIDEBAR_SPLIT_RATIO_KEY, String(finalHeight / availableHeight));
    }
  };

  resizer.addEventListener('mousedown', (e) => {
    if (resizer.classList.contains('hidden') || writeSection.classList.contains('hidden')) return;
    e.preventDefault();
    startY = e.clientY;
    startHeight = readSection.getBoundingClientRect().height;
    resizer.classList.add('resizing');
    document.body.classList.add('resizing-sidebar-section');
    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mouseup', onMouseUp);
  });

  resizer.addEventListener('dblclick', resetSidebarSplit);

  resizer.addEventListener('keydown', (e) => {
    if (resizer.classList.contains('hidden') || writeSection.classList.contains('hidden')) return;
    if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown' && e.key !== 'Home' && e.key !== 'End') return;

    e.preventDefault();
    const availableHeight = getSidebarSplitAvailableHeight();
    if (availableHeight <= 0) return;

    if (e.key === 'Home') {
      setSidebarSplitHeight(SIDEBAR_SECTION_MIN_HEIGHT, true);
      return;
    }
    if (e.key === 'End') {
      setSidebarSplitHeight(availableHeight - SIDEBAR_SECTION_MIN_HEIGHT, true);
      return;
    }

    const step = e.shiftKey ? 40 : 10;
    const direction = e.key === 'ArrowUp' ? -1 : 1;
    const currentHeight = readSection.getBoundingClientRect().height;
    setSidebarSplitHeight(currentHeight + (step * direction), true);
  });
}

// サイドバーの開閉
function toggleSidebar() {
  const sidebar = document.getElementById('sidebar');
  const openBtn = document.getElementById('sidebar-open-btn');
  const collapsed = sidebar.classList.toggle('collapsed');
  if (collapsed) {
    openBtn.classList.remove('hidden');
    lsSet(SIDEBAR_COLLAPSED_KEY, '1');
  } else {
    openBtn.classList.add('hidden');
    lsRemove(SIDEBAR_COLLAPSED_KEY);
  }
}

// ===== 編集ペイン =====

// 自動保存の間隔（ミリ秒）
const AUTO_SAVE_INTERVAL_MS = 5000;
let autoSaveTimer = null;

// 編集ペイン（textarea のショートカット、入力時の保存ボタン有効化、auto-grow）の初期化
function initEditPane() {
  const textarea = document.getElementById('edit-textarea');
  const saveBtn = document.getElementById('edit-save-btn');
  if (!textarea || !saveBtn) return;
  textarea.addEventListener('keydown', (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === 's') {
      e.preventDefault();
      saveCurrentEdit().catch(() => {});
    }
  });
  textarea.addEventListener('input', () => {
    autoGrowEditTextarea();
    if (currentEditFile) {
      editDirty = true;
      saveBtn.disabled = false;
    }
  });

  // 5 秒ごとの自動保存（dirty な場合のみ実行）
  if (!autoSaveTimer) {
    autoSaveTimer = setInterval(() => {
      if (currentEditFile && editDirty) {
        saveCurrentEdit({ silent: true }).catch(() => {});
      }
    }, AUTO_SAVE_INTERVAL_MS);
  }
}

// textarea の高さを scrollHeight に合わせる
// height='auto' で一旦縮めると親 .edit-area の scrollHeight も縮み、
// scrollTop が 0 にクランプされてしまうので、前後でスクロール位置を退避・復元する。
function autoGrowEditTextarea() {
  const textarea = document.getElementById('edit-textarea');
  if (!textarea) return;
  const area = document.getElementById('edit-area');
  const savedScrollTop = area ? area.scrollTop : 0;
  textarea.style.height = 'auto';
  textarea.style.height = textarea.scrollHeight + 'px';
  if (area) area.scrollTop = savedScrollTop;
}

// 編集ツリーのファイル選択（右ペインに編集 UI を表示）
async function selectEditFile(path, rowElement, skipURLUpdate = false, rootId = firstWriteRootId()) {
  currentEditRoot = rootId;
  // 編集ツリー内の以前の選択を解除
  document.querySelectorAll('.sidebar-section-write .file-item.selected').forEach(el => {
    el.classList.remove('selected');
  });
  if (rowElement) rowElement.classList.add('selected');

  // ダーティな未保存変更があれば保存しておく（失敗しても切替は続ける）
  if (currentEditFile && editDirty) {
    try { await saveCurrentEdit(); } catch (e) { /* ignore */ }
  }

  await loadEditFile(path);

  if (!skipURLUpdate) {
    updateEditURL(path);
    // ユーザー操作起点ならスマホ表示で編集ペインに切替
    setMobilePane('write');
  }
}

// パスから編集ファイルを選択（URL 復元用）
async function selectEditFileByPath(path) {
  const containerId = writeFileListIdForRoot(currentEditRoot);
  const fileItem = document.querySelector(`#${containerId} .file-item[data-path="${CSS.escape(path)}"]`);
  if (fileItem) {
    await selectEditFile(path, fileItem, true, currentEditRoot);
    // URL を正規化（replaceState で履歴を上書き）
    replaceEditURL(path);
  } else {
    console.warn('指定された編集ファイルがツリーに見つかりません:', path);
  }
}

// 指定パスを編集ペインに読み込む
async function loadEditFile(path) {
  const textarea = document.getElementById('edit-textarea');
  const empty = document.getElementById('edit-empty-state');
  const filenameEl = document.getElementById('edit-filename');
  const saveBtn = document.getElementById('edit-save-btn');

  try {
    const res = await fetch(`/api/raw?root=${encodeURIComponent(currentEditRoot)}&path=${encodeURIComponent(path)}`);
    if (!res.ok) {
      // 404 → 新規作成扱いとして空内容で開く
      if (res.status === 404) {
        textarea.value = '';
      } else {
        showEditStatus('読み込み失敗', true);
        return;
      }
    } else {
      textarea.value = await res.text();
    }
    currentEditFile = path;
    editDirty = false;
    const root = writeRootById(currentEditRoot);
    filenameEl.textContent = root && root.name ? `${root.name}/${path}` : path;
    const copyBtn = document.getElementById('edit-copy-btn');
    if (copyBtn) copyBtn.disabled = false;
    empty.classList.add('hidden');
    textarea.classList.remove('hidden');
    saveBtn.disabled = true; // 開いた直後は dirty=false
    autoGrowEditTextarea();
    textarea.focus();
  } catch (e) {
    showEditStatus('読み込み失敗', true);
  }
}

// 現在開いている編集ファイルを保存（Cmd+S・「保存」ボタン・自動保存から呼ばれる）。
// silent=true（自動保存）では成功時のステータス表示を抑止する。
async function saveCurrentEdit({ silent = false } = {}) {
  if (!currentEditFile) return;
  const textarea = document.getElementById('edit-textarea');
  const saveBtn = document.getElementById('edit-save-btn');
  try {
    const res = await fetch(`/api/file?root=${encodeURIComponent(currentEditRoot)}&path=${encodeURIComponent(currentEditFile)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'text/plain; charset=utf-8' },
      body: textarea.value,
    });
    if (!res.ok) {
      showEditStatus('保存に失敗しました', true);
      throw new Error('save failed');
    }
    await res.json();
    editDirty = false;
    saveBtn.disabled = true;
    if (!silent) showEditStatus('保存しました');
  } catch (e) {
    showEditStatus('保存に失敗しました', true);
    throw e;
  }
}

// 編集ヘッダーに一時的なステータスを表示（数秒で消える）
function showEditStatus(message, isError = false) {
  const el = document.getElementById('edit-status');
  if (!el) return;
  el.textContent = message;
  el.classList.toggle('edit-status-error', isError);
  el.classList.remove('hidden');
  clearTimeout(showEditStatus._timer);
  showEditStatus._timer = setTimeout(() => {
    el.classList.add('hidden');
  }, 2500);
}

// 編集ツリーの折り畳み状態を localStorage に永続化
function collapsedWriteStorageKey(rootId) {
  return rootId === firstWriteRootId() ? COLLAPSED_WRITE_DIRS_KEY : `${COLLAPSED_WRITE_DIRS_KEY}.${rootId}`;
}
function saveCollapsedWriteDirs(rootId = firstWriteRootId()) {
  lsSet(collapsedWriteStorageKey(rootId), JSON.stringify([...collapsedWriteSetForRoot(rootId)]));
}
function loadCollapsedWriteDirs(rootId = firstWriteRootId()) {
  const collapsedSet = collapsedWriteSetForRoot(rootId);
  const raw = lsGet(collapsedWriteStorageKey(rootId));
  if (!raw) return;
  try {
    const arr = JSON.parse(raw);
    if (Array.isArray(arr)) {
      arr.forEach(p => { if (typeof p === 'string') collapsedSet.add(p); });
    }
  } catch (e) { /* ignore */ }
}

// 差分表示のトグル
function toggleDiffView() {
  isDiffMode = !isDiffMode;
  updateDiffView();
}

// 差分表示の更新
function updateDiffView() {
  const documentEl = document.getElementById('document');
  const diffViewEl = document.getElementById('diff-view');
  const diffBtnText = document.getElementById('diff-btn-text');
  const toggleBtn = document.getElementById('toggle-diff-btn');

  if (isDiffMode && currentDiffData && currentDiffData.hasDiff) {
    // 差分表示モード
    documentEl.classList.add('hidden');
    diffViewEl.classList.remove('hidden');
    diffBtnText.textContent = 'ドキュメントを表示';
    toggleBtn.classList.add('active');
    renderDiff(currentDiffData);
  } else {
    // 通常表示モード
    documentEl.classList.remove('hidden');
    diffViewEl.classList.add('hidden');
    diffBtnText.textContent = '差分を表示';
    toggleBtn.classList.remove('active');
    isDiffMode = false;
  }
}

// 差分ボタンバーの表示/非表示
function updateDiffButtonBar() {
  const diffButtonBar = document.getElementById('diff-button-bar');

  if (currentDiffData && currentDiffData.hasDiff) {
    diffButtonBar.classList.remove('hidden');
  } else {
    diffButtonBar.classList.add('hidden');
    // 差分がない場合は通常表示に戻す
    if (isDiffMode) {
      isDiffMode = false;
      updateDiffView();
    }
  }
}

// 差分データを読み込み
async function loadDiff(path) {
  try {
    let url = `/api/diff?path=${encodeURIComponent(path)}`;
    url = appendRootQuery(url, currentReadRoot);
    const res = await fetch(url);
    if (!res.ok) {
      currentDiffData = null;
      return;
    }
    currentDiffData = await res.json();
  } catch (err) {
    console.error('差分の読み込みに失敗しました:', err);
    currentDiffData = null;
  }
}

// 差分を描画
function renderDiff(diffData) {
  const leftPanel = document.getElementById('diff-left');
  const rightPanel = document.getElementById('diff-right');
  const rightHeader = document.getElementById('diff-right-header');

  // ヘッダーを更新
  rightHeader.textContent = diffData.currentWorktree;

  // 左右パネルをクリア
  leftPanel.innerHTML = '';
  rightPanel.innerHTML = '';

  // 差分行を描画
  diffData.lines.forEach(line => {
    const leftLine = createDiffLine(line, 'base');
    const rightLine = createDiffLine(line, 'current');

    leftPanel.appendChild(leftLine);
    rightPanel.appendChild(rightLine);
  });
}

// 差分行を作成
function createDiffLine(line, side) {
  const div = document.createElement('div');
  div.className = 'diff-line';

  const lineNum = document.createElement('span');
  lineNum.className = 'diff-line-num';

  const content = document.createElement('span');
  content.className = 'diff-line-content';

  if (line.type === 'unchanged') {
    div.classList.add('diff-line-unchanged');
    if (side === 'base') {
      lineNum.textContent = line.baseLineNum || '';
    } else {
      lineNum.textContent = line.currentLineNum || '';
    }
    content.textContent = line.text;
  } else if (line.type === 'added') {
    if (side === 'base') {
      // 左パネル（ベース）では空行として表示
      div.classList.add('diff-line-empty');
      lineNum.textContent = '';
      content.textContent = '';
    } else {
      // 右パネル（現在）では追加行として表示
      div.classList.add('diff-line-added');
      lineNum.textContent = line.currentLineNum || '';
      content.textContent = line.text;
    }
  } else if (line.type === 'removed') {
    if (side === 'base') {
      // 左パネル（ベース）では削除行として表示
      div.classList.add('diff-line-removed');
      lineNum.textContent = line.baseLineNum || '';
      content.textContent = line.text;
    } else {
      // 右パネル（現在）では空行として表示
      div.classList.add('diff-line-empty');
      lineNum.textContent = '';
      content.textContent = '';
    }
  }

  div.appendChild(lineNum);
  div.appendChild(content);
  return div;
}

// 閲覧側の選択を解除
function clearSelection() {
  document.querySelectorAll('.sidebar-section-read .file-item.selected').forEach(el => {
    el.classList.remove('selected');
  });

  currentDocument = null;
  currentReadRoot = firstReadRootId();
  lastModifiedAt = null;
  isDiffMode = false;
  currentDiffData = null;

  document.getElementById('empty-state').classList.remove('hidden');
  document.getElementById('document').classList.add('hidden');
  document.getElementById('diff-view').classList.add('hidden');
  document.getElementById('worktree-tabs').classList.add('hidden');
  document.getElementById('diff-button-bar').classList.add('hidden');
  updateReadHeader(null);
}

// 閲覧ペインのヘッダー（ファイル名・コピーボタン）を更新
function updateReadHeader(path) {
  const nameEl = document.getElementById('read-filename');
  const btn = document.getElementById('read-copy-btn');
  if (!nameEl || !btn) return;
  if (path) {
    nameEl.textContent = path;
    nameEl.title = path;
    btn.disabled = false;
  } else {
    nameEl.textContent = 'ファイルを選択してください';
    nameEl.removeAttribute('title');
    btn.disabled = true;
  }
}

// ブラウザの戻る/進むボタンに対応
window.addEventListener('popstate', async (event) => {
  const state = event.state;

  if (state && state.file && isValidPath(state.file)) {
    const nextRoot = isReadableRoot(state.root) ? (state.root || firstReadRootId()) : firstReadRootId();
    currentReadRoot = nextRoot;
    await loadWorktrees(currentReadRoot);

    // worktreeを先に設定
    if (state.worktree && worktrees.some(wt => wt.name === state.worktree)) {
      await selectWorktree(state.worktree, true);
    }

    // ファイルを選択（URL更新はしない）
    const containerId = fileListIdForRoot(currentReadRoot);
    const fileItem = document.querySelector(`#${containerId} .file-item[data-path="${CSS.escape(state.file)}"]`);
    if (fileItem) {
      await selectFile(state.file, fileItem, true, currentReadRoot);
    }
  } else {
    // パラメータがない場合は選択解除
    clearSelection();
  }

  // 編集ファイルの復元（writeEnabled 時のみ、状態が変わった場合のみ反映）
  if (writeEnabled) {
    const editPath = state && state.editFile;
    const editRoot = state && writeRoots.some(root => root.id === state.editRoot) ? state.editRoot : firstWriteRootId();
    if (editPath && isValidPath(editPath) && (editPath !== currentEditFile || editRoot !== currentEditRoot)) {
      currentEditRoot = editRoot;
      const editItem = document.querySelector(`#${writeFileListIdForRoot(currentEditRoot)} .file-item[data-path="${CSS.escape(editPath)}"]`);
      if (editItem) {
        await selectEditFile(editPath, editItem, true, currentEditRoot);
      }
    }
  }
});
