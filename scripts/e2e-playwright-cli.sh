#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
SERVER_PID=""
SESSION="reader-e2e"
export GOCACHE="${TMP_DIR}/gocache"

cleanup() {
	if [ -n "${SERVER_PID}" ]; then
		kill "${SERVER_PID}" >/dev/null 2>&1 || true
		wait "${SERVER_PID}" >/dev/null 2>&1 || true
	fi
	playwright-cli -s="${SESSION}" close >/dev/null 2>&1 || true
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

if ! command -v playwright-cli >/dev/null 2>&1; then
	echo "ERROR: playwright-cli が見つかりません" >&2
	exit 1
fi

repo="${TMP_DIR}/repo"
read_a="${TMP_DIR}/read-a"
read_b="${TMP_DIR}/read-b"
write_a="${TMP_DIR}/write-a"
write_b="${TMP_DIR}/write-b"
log="${TMP_DIR}/reader.log"
port="${E2E_PORT:-}"

if [ -z "${port}" ]; then
	port="$(node -e "const net=require('net'); const s=net.createServer(); s.listen(0,'127.0.0.1',()=>{console.log(s.address().port); s.close();});")"
fi

mkdir -p "${repo}" "${read_a}/docs" "${read_b}" "${write_a}" "${write_b}"
mkdir -p "${GOCACHE}"

git -C "${repo}" init -q
git -C "${repo}" config user.email test@example.com
git -C "${repo}" config user.name "Test User"
git -C "${repo}" checkout -q -b main
printf '# current root\n' > "${repo}/README.md"
git -C "${repo}" add .
git -C "${repo}" commit -q -m init

printf '# alpha read A\n' > "${read_a}/alpha.md"
printf '# zeta read A\n' > "${read_a}/zeta.md"
printf '# docs alpha read A\n' > "${read_a}/docs/alpha.md"
printf '# docs zeta read A\n' > "${read_a}/docs/zeta.md"
printf '# beta read B\n' > "${read_b}/beta.md"
printf '<!doctype html><title>page</title>\n' > "${read_b}/page.html"
printf '# alpha write A\n' > "${write_a}/alpha.md"
printf '# zeta write A\n' > "${write_a}/zeta.md"
printf '# beta write B\n' > "${write_b}/beta.md"
printf '<!doctype html><title>write page</title>\n' > "${write_b}/page.html"

(
	cd "${ROOT_DIR}"
	go run . -no-open -host 127.0.0.1 -port "${port}" \
		-include "*.md" -include "*.html" \
		-read-r "${read_a}" -read "${read_b}" \
		-write-r "${write_a}" -write "${write_b}" \
		"${repo}"
) >"${log}" 2>&1 &
SERVER_PID=$!

url=""
for _ in $(seq 1 100); do
	if ! kill -0 "${SERVER_PID}" >/dev/null 2>&1; then
		echo "ERROR: reader server failed to start" >&2
		cat "${log}" >&2
		exit 1
	fi
	url="$(grep -Eo 'http://127\.0\.0\.1:[0-9]+' "${log}" | head -n 1 || true)"
	if [ -n "${url}" ]; then
		break
	fi
	sleep 0.1
done

if [ -z "${url}" ]; then
	echo "ERROR: reader server URL was not printed" >&2
	cat "${log}" >&2
	exit 1
fi

playwright-cli -s="${SESSION}" open "${url}" >/dev/null

wait_for_ui_ready() {
	for _ in $(seq 1 100); do
		ready_json="$(
			playwright-cli -s="${SESSION}" --raw eval "JSON.stringify({
				readRootCount: document.querySelectorAll('#file-list > .root-accordion').length,
				writeRootCount: document.querySelectorAll('#write-file-list > .root-accordion').length,
				secondWriteFileReady: !!document.querySelector(\"#write-root-file-list-write-2 .file-item[data-path='beta.md']\")
			})" 2>/dev/null || true
		)"
		if RESULT="${ready_json}" node -e '
let r = JSON.parse(process.env.RESULT || "{}");
if (typeof r === "string") r = JSON.parse(r);
process.exit(r.readRootCount === 2 && r.writeRootCount === 2 && r.secondWriteFileReady ? 0 : 1);
' >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.1
	done

	echo "ERROR: reader UI was not ready" >&2
	playwright-cli -s="${SESSION}" --raw eval "document.body.innerText" >&2 || true
	exit 1
}

wait_for_ui_ready

initial_json="$(
	playwright-cli -s="${SESSION}" --raw eval "JSON.stringify({
		sectionTitles: Array.from(document.querySelectorAll('.sidebar-section-title-text')).map(e => e.textContent.trim()),
		readRoots: Array.from(document.querySelectorAll('#file-list > .root-accordion .root-accordion-label')).map(e => e.textContent.trim()),
		writeRoots: Array.from(document.querySelectorAll('#write-file-list > .root-accordion .root-accordion-label')).map(e => e.textContent.trim()),
		firstReadFiles: Array.from(document.querySelectorAll('#read-root-file-list-read .file-item .file-name')).map(e => e.textContent.trim()),
		secondReadFiles: Array.from(document.querySelectorAll('#read-root-file-list-read-2 .file-item .file-name')).map(e => e.textContent.trim()),
		secondReadHtmlLink: (() => {
			const icon = document.querySelector('#read-root-file-list-read-2 .file-item[data-path=\"page.html\"] .html-url-link');
			const link = icon?.closest('.file-label');
			return icon && link && {
				href: link.getAttribute('href'),
				target: link.getAttribute('target'),
				rel: link.getAttribute('rel'),
				inlineWithName: icon.parentElement?.classList.contains('file-label') && icon.previousElementSibling?.classList.contains('file-name'),
				labelTag: link.tagName
			};
		})(),
		secondReadHtmlClickOpen: (() => {
			const link = document.querySelector('#read-root-file-list-read-2 .file-item[data-path=\"page.html\"] .file-label');
			const originalOpen = window.open;
			let opened = null;
			window.open = (url, target, features) => {
				opened = { url, target, features };
				return {};
			};
			link?.click();
			window.open = originalOpen;
			return opened;
		})(),
		firstWriteFiles: Array.from(document.querySelectorAll('#write-root-file-list-write .file-item .file-name')).map(e => e.textContent.trim()),
		secondWriteFiles: Array.from(document.querySelectorAll('#write-root-file-list-write-2 .file-item .file-name')).map(e => e.textContent.trim()),
		secondWriteHtmlLink: (() => {
			const icon = document.querySelector('#write-root-file-list-write-2 .file-item[data-path=\"page.html\"] .html-url-link');
			const link = icon?.closest('.file-label');
			return icon && link && {
				href: link.getAttribute('href'),
				target: link.getAttribute('target'),
				rel: link.getAttribute('rel'),
				inlineWithName: icon.parentElement?.classList.contains('file-label') && icon.previousElementSibling?.classList.contains('file-name'),
				labelTag: link.tagName
			};
		})(),
		firstReadDirLabels: Array.from(document.querySelectorAll('#read-root-file-list-read .dir-label')).map(e => e.textContent.trim()),
		secondReadDirLabels: Array.from(document.querySelectorAll('#read-root-file-list-read-2 .dir-label')).map(e => e.textContent.trim()),
		firstWriteDirLabels: Array.from(document.querySelectorAll('#write-root-file-list-write .dir-label')).map(e => e.textContent.trim()),
		secondWriteDirLabels: Array.from(document.querySelectorAll('#write-root-file-list-write-2 .dir-label')).map(e => e.textContent.trim()),
		rootActionCount: document.querySelectorAll('.root-accordion-action').length,
		headerToggleAllButtons: [
			!!document.querySelector('#toggle-all-read-btn'),
			!!document.querySelector('#toggle-all-write-btn')
		],
		allReadPaths: Array.from(document.querySelectorAll('.sidebar-section-read .file-item')).map(e => e.dataset.path),
		readSectionCount: document.querySelectorAll('.sidebar-section-read').length,
		writeSectionCount: document.querySelectorAll('.sidebar-section-write').length
	})"
)"

RESULT="${initial_json}" node -e '
let r = JSON.parse(process.env.RESULT);
if (typeof r === "string") r = JSON.parse(r);
const eq = (a, b) => JSON.stringify(a) === JSON.stringify(b);
function assert(ok, msg) { if (!ok) throw new Error(msg + "\n" + JSON.stringify(r, null, 2)); }
assert(eq(r.sectionTitles, ["pull requests", "readings", "writings"]), "section bars should be singular");
assert(eq(r.readRoots, ["read-a", "read-b"]), "read accordions should follow command order");
assert(eq(r.writeRoots, ["write-a", "write-b"]), "write accordions should follow command order");
assert(eq(r.firstReadFiles, ["zeta.md", "alpha.md", "zeta.md", "alpha.md"]), "read-r tree should be descending");
assert(eq(r.secondReadFiles, ["beta.md", "page.html"]), "second read tree should render");
assert(r.secondReadHtmlLink && r.secondReadHtmlLink.href === "/html/read-2/page.html", "html files in read sidebar should link to html preview");
assert(r.secondReadHtmlLink.target === "_blank", "html sidebar links should open in a new tab");
assert(r.secondReadHtmlLink.rel === "noopener noreferrer", "html sidebar links should protect the opener");
assert(r.secondReadHtmlLink.inlineWithName, "html sidebar link should be immediately after the file name");
assert(r.secondReadHtmlLink.labelTag === "A", "html file label should be a native link");
assert(r.secondReadHtmlClickOpen && r.secondReadHtmlClickOpen.url === "/html/read-2/page.html", "clicking html file label should request the html preview URL");
assert(r.secondReadHtmlClickOpen.target === "_blank", "clicking html file label should request a new tab");
assert(eq(r.firstWriteFiles, ["zeta.md", "alpha.md"]), "write-r tree should be descending");
assert(eq(r.secondWriteFiles, ["beta.md", "page.html"]), "second write tree should render");
assert(r.secondWriteHtmlLink && r.secondWriteHtmlLink.href === "/html/write-2/page.html", "html files in write sidebar should link to html preview");
assert(r.secondWriteHtmlLink.target === "_blank", "write html sidebar links should open in a new tab");
assert(r.secondWriteHtmlLink.rel === "noopener noreferrer", "write html sidebar links should protect the opener");
assert(r.secondWriteHtmlLink.inlineWithName, "write html sidebar link should be immediately after the file name");
assert(r.secondWriteHtmlLink.labelTag === "A", "write html file label should be a native link");
assert(eq(r.firstReadDirLabels, ["▶docs/"]), "read root name should not be duplicated inside first read accordion");
assert(eq(r.secondReadDirLabels, []), "root-only read files should not render duplicate read root directory");
assert(eq(r.firstWriteDirLabels, []), "root-only write files should not render duplicate write root directory");
assert(eq(r.secondWriteDirLabels, []), "root-only second write files should not render duplicate write root directory");
assert(r.rootActionCount === 0, "per-root expand/collapse buttons should not be rendered");
assert(eq(r.headerToggleAllButtons, [true, true]), "section header expand/collapse buttons should remain");
assert(!r.allReadPaths.includes("README.md"), "current directory must not be shown when -read is specified");
assert(r.readSectionCount === 1, "readings should be one section");
assert(r.writeSectionCount === 1, "writings should be one section");
'

playwright-cli -s="${SESSION}" --raw eval "(() => {
	const state = JSON.parse(localStorage.getItem('reader.readState.v1') || '{\"schema\":1,\"scopes\":{}}');
	state.schema = 1;
	state.scopes = state.scopes || {};
	state.scopes['read-2'] = { baselineVersionMs: 0, files: {} };
	localStorage.setItem('reader.readState.v1', JSON.stringify(state));
	location.reload();
})()" >/dev/null

wait_for_ui_ready

html_read_json="$(
	playwright-cli -s="${SESSION}" --raw eval "JSON.stringify((() => {
		const rowSelector = '#read-root-file-list-read-2 .file-item[data-path=\"page.html\"]';
		const link = document.querySelector(rowSelector + ' .file-label');
		const countText = () => document.querySelector('#unread-total-count')?.textContent.trim();
		const rowUnread = () => document.querySelector(rowSelector)?.classList.contains('unread');
		const before = { pageUnread: rowUnread(), unreadTotal: countText() };
		const originalOpen = window.open;
		let opened = null;
		window.open = (url, target, features) => {
			opened = { url, target, features };
			return {};
		};
		link?.click();
		window.open = originalOpen;
		const state = JSON.parse(localStorage.getItem('reader.readState.v1') || '{}');
		return {
			before,
			after: { pageUnread: rowUnread(), unreadTotal: countText() },
			opened,
			readEntry: state.scopes?.['read-2']?.files?.['page.html'] || null
		};
	})())"
)"

RESULT="${html_read_json}" node -e '
let r = JSON.parse(process.env.RESULT);
if (typeof r === "string") r = JSON.parse(r);
function assert(ok, msg) { if (!ok) throw new Error(msg + "\n" + JSON.stringify(r, null, 2)); }
assert(r.before.pageUnread === true, "html file should be unread before opening");
assert(r.before.unreadTotal === "2", "second read root should start with two unread files");
assert(r.opened && r.opened.url === "/html/read-2/page.html", "clicking unread html file should still open preview URL");
assert(r.after.pageUnread === false, "html file should become read after opening");
assert(r.after.unreadTotal === "1", "unread total should decrement after opening html");
assert(r.readEntry && Number(r.readEntry.version?.modifiedAtMs) > 0, "html read state should store file version");
'

playwright-cli -s="${SESSION}" click "#toggle-all-read-btn" >/dev/null
playwright-cli -s="${SESSION}" click "#toggle-all-write-btn" >/dev/null

collapsed_json="$(
	playwright-cli -s="${SESSION}" --raw eval "JSON.stringify({
		readCollapsed: Array.from(document.querySelectorAll('#file-list > .root-accordion')).map(e => e.classList.contains('collapsed')),
		writeCollapsed: Array.from(document.querySelectorAll('#write-file-list > .root-accordion')).map(e => e.classList.contains('collapsed')),
		readButton: document.querySelector('#toggle-all-read-btn').textContent,
		writeButton: document.querySelector('#toggle-all-write-btn').textContent
	})"
)"

RESULT="${collapsed_json}" node -e '
let r = JSON.parse(process.env.RESULT);
if (typeof r === "string") r = JSON.parse(r);
const eq = (a, b) => JSON.stringify(a) === JSON.stringify(b);
function assert(ok, msg) { if (!ok) throw new Error(msg + "\n" + JSON.stringify(r, null, 2)); }
assert(eq(r.readCollapsed, [true, true]), "readings header toggle should collapse all read root accordions");
assert(eq(r.writeCollapsed, [true, true]), "writings header toggle should collapse all write root accordions");
assert(r.readButton === "⊞" && r.writeButton === "⊞", "collapsed section buttons should switch to expand action");
'

playwright-cli -s="${SESSION}" click "#toggle-all-read-btn" >/dev/null
playwright-cli -s="${SESSION}" click "#toggle-all-write-btn" >/dev/null

expanded_json="$(
	playwright-cli -s="${SESSION}" --raw eval "JSON.stringify({
		readCollapsed: Array.from(document.querySelectorAll('#file-list > .root-accordion')).map(e => e.classList.contains('collapsed')),
		writeCollapsed: Array.from(document.querySelectorAll('#write-file-list > .root-accordion')).map(e => e.classList.contains('collapsed')),
		readButton: document.querySelector('#toggle-all-read-btn').textContent,
		writeButton: document.querySelector('#toggle-all-write-btn').textContent
	})"
)"

RESULT="${expanded_json}" node -e '
let r = JSON.parse(process.env.RESULT);
if (typeof r === "string") r = JSON.parse(r);
const eq = (a, b) => JSON.stringify(a) === JSON.stringify(b);
function assert(ok, msg) { if (!ok) throw new Error(msg + "\n" + JSON.stringify(r, null, 2)); }
assert(eq(r.readCollapsed, [false, false]), "readings header toggle should expand all read root accordions");
assert(eq(r.writeCollapsed, [false, false]), "writings header toggle should expand all write root accordions");
assert(r.readButton === "⊟" && r.writeButton === "⊟", "expanded section buttons should switch to collapse action");
'

playwright-cli -s="${SESSION}" click "#toggle-read-section-btn" >/dev/null
playwright-cli -s="${SESSION}" click "#toggle-write-section-btn" >/dev/null

hidden_json="$(
	playwright-cli -s="${SESSION}" --raw eval "JSON.stringify({
		readHidden: document.querySelector('#sidebar-section-read').classList.contains('section-hidden'),
		writeHidden: document.querySelector('#sidebar-section-write').classList.contains('section-hidden'),
		readDisplay: getComputedStyle(document.querySelector('#file-list')).display,
		writeDisplay: getComputedStyle(document.querySelector('#write-file-list')).display,
		readButton: document.querySelector('#toggle-read-section-btn').textContent,
		writeButton: document.querySelector('#toggle-write-section-btn').textContent
	})"
)"

RESULT="${hidden_json}" node -e '
let r = JSON.parse(process.env.RESULT);
if (typeof r === "string") r = JSON.parse(r);
function assert(ok, msg) { if (!ok) throw new Error(msg + "\n" + JSON.stringify(r, null, 2)); }
assert(r.readHidden && r.writeHidden, "section toggles should hide both sections");
assert(r.readDisplay === "none" && r.writeDisplay === "none", "hidden sections should hide lists");
assert(r.readButton === "▸" && r.writeButton === "▸", "hidden section buttons should point right");
'

playwright-cli -s="${SESSION}" click "#toggle-read-section-btn" >/dev/null
playwright-cli -s="${SESSION}" click "#toggle-write-section-btn" >/dev/null
playwright-cli -s="${SESSION}" click "#write-root-file-list-write-2 .file-item[data-path='beta.md']" >/dev/null

edit_json="$(
	playwright-cli -s="${SESSION}" --raw eval "JSON.stringify({
		url: location.search,
		editHeader: document.querySelector('#edit-filename').textContent.trim(),
		editValue: document.querySelector('#edit-textarea').value.trim(),
		selectedPaths: Array.from(document.querySelectorAll('#write-root-file-list-write-2 .file-item.selected')).map(e => e.dataset.path)
	})"
)"

RESULT="${edit_json}" node -e '
let r = JSON.parse(process.env.RESULT);
if (typeof r === "string") r = JSON.parse(r);
function assert(ok, msg) { if (!ok) throw new Error(msg + "\n" + JSON.stringify(r, null, 2)); }
assert(r.url.includes("editFile=beta.md") && r.url.includes("editRoot=write-2"), "edit URL should preserve write root");
assert(r.editHeader === "write-b/beta.md", "edit header should show selected write root");
assert(r.editValue === "# beta write B", "edit pane should load second write root content");
assert(JSON.stringify(r.selectedPaths) === JSON.stringify(["beta.md"]), "second write root row should be selected");
'

playwright-cli -s="${SESSION}" resize 390 844 >/dev/null
playwright-cli -s="${SESSION}" goto "${url}?editFile=beta.md&editRoot=write-2" >/dev/null

mobile_edit_url_json=""
for _ in $(seq 1 100); do
	mobile_edit_url_json="$(
		playwright-cli -s="${SESSION}" --raw eval "JSON.stringify({
			url: location.search,
			sidebarCollapsed: document.querySelector('#sidebar').classList.contains('collapsed'),
			mobileShowWrite: document.querySelector('.content-wrapper').classList.contains('mobile-show-write'),
			readDisplay: getComputedStyle(document.querySelector('#pane-read')).display,
			writeDisplay: getComputedStyle(document.querySelector('#pane-write')).display,
			editHeader: document.querySelector('#edit-filename').textContent.trim(),
			editValue: document.querySelector('#edit-textarea').value.trim(),
			selectedPaths: Array.from(document.querySelectorAll('#write-root-file-list-write-2 .file-item.selected')).map(e => e.dataset.path)
		})"
	)"
	if RESULT="${mobile_edit_url_json}" node -e '
let r = JSON.parse(process.env.RESULT);
if (typeof r === "string") r = JSON.parse(r);
process.exit(r.editHeader === "write-b/beta.md" ? 0 : 1);
' >/dev/null 2>&1; then
		break
	fi
	sleep 0.1
done

RESULT="${mobile_edit_url_json}" node -e '
let r = JSON.parse(process.env.RESULT);
if (typeof r === "string") r = JSON.parse(r);
function assert(ok, msg) { if (!ok) throw new Error(msg + "\n" + JSON.stringify(r, null, 2)); }
assert(r.url.includes("editFile=beta.md") && r.url.includes("editRoot=write-2"), "mobile edit URL should preserve write root");
assert(r.sidebarCollapsed, "mobile edit URL should open directly to detail view");
assert(r.mobileShowWrite, "mobile edit URL should show the write pane");
assert(r.readDisplay === "none" && r.writeDisplay === "flex", "mobile edit URL should hide read pane and show write pane");
assert(r.editHeader === "write-b/beta.md", "mobile edit URL should show selected write root");
assert(r.editValue === "# beta write B", "mobile edit URL should load second write root content");
assert(JSON.stringify(r.selectedPaths) === JSON.stringify(["beta.md"]), "mobile edit URL should select second write root row");
'

echo "playwright-cli e2e passed"
