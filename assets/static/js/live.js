/* GoBBS 实时局部刷新（SSE）：
   - 帖子页：别人新回复 / 编辑楼层 / 删除楼层 → 无整页刷新，局部替换
   - 版块页 + 首页：主题列表行 / 版块行随发帖动作即时更新 */
(function () {
	'use strict';

	var zone = document.querySelector('[data-live]');
	var wp = document.getElementById('wp');
	if ((!zone && !(wp && wp.dataset.liveUser)) || !window.EventSource) return;
	var topics = zone ? zone.getAttribute('data-live') : '';
	if (wp && wp.dataset.liveUser) {
		topics = topics ? topics + '&' : '';
		topics += 'user=' + wp.dataset.liveUser;
	}
	var es = new EventSource('/api/live?' + topics);
	window.addEventListener('beforeunload', function () { es.close(); });

	es.onmessage = function (e) {
		var ev;
		try { ev = JSON.parse(e.data); } catch (err) { return; }
		switch (ev.type) {
			case 'post.new':    onPostNew(ev); break;
			case 'post.edit':   onPostEdit(ev); break;
			case 'post.delete': onPostDelete(ev); break;
			case 'post.like':   onPostLike(ev); break;
			case 'thread.new':    onThreadNew(ev); break;
			case 'thread.update': onThreadUpdate(ev); break;
			case 'thread.delete': onThreadDelete(ev); break;
			case 'thread.deleted': onSelfThreadDeleted(ev); break;
			case 'notify': onNotify(ev); break;
		}
	};

	/* ---- 片段解析 ---- */

	function fromHTML(html, wantTbody) {
		var tpl = document.createElement('template');
		if (wantTbody) {
			// tbody 只有在 table 上下文中解析才不会丢
			tpl.innerHTML = '<table>' + html + '</table>';
			return tpl.content.querySelector('tbody');
		}
		tpl.innerHTML = html;
		return tpl.content.firstElementChild;
	}

	function highlight(el) {
		if (!el) return;
		el.classList.add('flash-new');
		setTimeout(function () { el.classList.remove('flash-new'); }, 2600);
	}

	function pill(text, href) {
		var el = document.createElement('div');
		el.className = 'live-pill';
		el.textContent = text;
		el.addEventListener('click', function () { window.location.href = href; });
		document.body.appendChild(el);
	}

	/* ---- 帖子页 ---- */

	var list = document.getElementById('postlist');

	function onPostNew(ev) {
		if (!list) return onForumUpdateRow(ev); // 兜底：非帖子页
		var page = +list.dataset.page, size = +list.dataset.pageSize;
		var inPage = ev.floor > (page - 1) * size && ev.floor <= page * size;
		if (inPage) {
			list.querySelector('#last') || list.insertAdjacentHTML('beforeend', '<div id="last"></div>');
			var node = fromHTML(ev.postHtml, false);
			if (node) {
				list.insertBefore(node, list.querySelector('#last'));
				highlight(node);
				node.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
			}
		} else {
			var lastPage = Math.ceil((ev.postCount || 0) / size) || 1;
			var href = '/thread-' + ev.tid + '-' + lastPage + '-1.html#post' + ev.pid;
			pill('有新回复，点击查看 ›', href);
		}
	}

	function onPostEdit(ev) {
		var cur = document.getElementById('post' + ev.pid);
		if (!cur) return;
		var node = fromHTML(ev.postHtml, false);
		if (node) {
			cur.replaceWith(node);
			highlight(node);
			pill('该楼层已被作者更新');
		}
	}

	function onPostDelete(ev) {
		var cur = document.getElementById('post' + ev.pid);
		if (cur) {
			cur.remove();
			pill('该楼层已被删除');
		}
	}

	function onNotify(ev) {
		var link = document.querySelector('.notify-link');
		if (link) {
			link.classList.add('has-unread');
			var num = link.querySelector('.notify-num');
			if (num) num.textContent = ev.notifyCount || '';
			else link.insertAdjacentHTML('beforeend', ' <em class="notify-num">' + (ev.notifyCount || 1) + '</em>');
		}
		if (ev.fromName) {
			pill(ev.fromName + ' 在帖子中提到了你，点击查看通知', '/notify');
		}
	}

	function onPostLike(ev) {
		var span = document.querySelector('[data-lc="' + ev.pid + '"]');
		if (span) span.textContent = ev.likeCount > 0 ? '👍 ×' + ev.likeCount : '';
	}

	function onSelfThreadDeleted() {
		if (!list) return;
		pill('该主题已被删除', '/');
		setTimeout(function () { window.location.href = '/'; }, 1500);
	}

	/* ---- 版块页 / 首页 ---- */

	function onForumUpdateRow(ev) {
		if (ev.threadRow) {
			var row = document.getElementById('row' + ev.tid);
			var node = fromHTML(ev.threadRow, true);
			if (node) {
				if (row) row.replaceWith(node);
				else {
					var table = document.getElementById('threadlisttableid');
					if (table) table.insertBefore(node, table.firstElementChild);
				}
				highlight(node);
			}
		}
		if (ev.forumRow) {
			var fidMatch = ev.forumRow.match(/data-fid="(\d+)"/);
			if (fidMatch) {
				var frow = document.getElementById('fl' + fidMatch[1]);
				if (frow) {
					var tmp = document.createElement('table');
					tmp.innerHTML = ev.forumRow;
					var newTr = tmp.firstElementChild;
					if (newTr && newTr.tagName === 'TR') {
						frow.replaceWith(newTr);
						highlight(newTr);
					}
				}
			}
		}
	}

	function onThreadNew(ev)    { onForumUpdateRow(ev); pill('有新主题发布'); }
	function onThreadUpdate(ev) { onForumUpdateRow(ev); }
	function onThreadDelete(ev) {
		var row = document.getElementById('row' + ev.tid);
		if (row) { row.remove(); pill('该主题已被删除'); }
	}
})();
