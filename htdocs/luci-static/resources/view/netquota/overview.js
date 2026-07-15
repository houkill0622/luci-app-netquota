// netquotad — 网络时长管理 LuCI 前端界面
'use strict';
'require view';
'require ui';
'require uci';
'require rpc';
'require poll';
'require fs';

var callFileExec = rpc.declare({
	object: 'file',
	method: 'exec',
	params: ['command', 'params'],
	expect: { '': {} }
});

function netquotad(args) {
	return callFileExec({ command: '/usr/bin/netquotad-cli', params: args }).then(function(res) {
		if (res.code !== 0) {
			throw new Error(['netquotad-cli 退出码 ', res.code, ': ', res.stderr || '未知错误'].join(''));
		}
		try {
			return JSON.parse(res.stdout);
		} catch(e) {
			return { _raw: res.stdout };
		}
	});
}

function macFmt(m) {
	if (!m) return '-';
	m = m.replace(/[^0-9A-Fa-f]/g, '').toUpperCase();
	var p = [];
	for (var i = 0; i < 12 && i < m.length; i += 2) p.push(m.substr(i, 2));
	return p.join(':');
}

function durFmt(m) {
	if (!m || m <= 0) return '0 分钟';
	if (m < 60) return m + ' 分钟';
	var h = Math.floor(m / 60), r = m % 60;
	return r > 0 ? h + ' 小时 ' + r + ' 分钟' : h + ' 小时';
}

function loadDevs() {
	var d = [];
	uci.sections('netquota', 'device', function(s) {
		d.push({ mac: s.mac || '', name: s.name || '', quota: parseInt(s.quota) || 0, mode: parseInt(s.mode) || 2, enabled: s.enabled === '1' || s.enabled === true });
	});
	return d;
}

var _tblDiv = null, _devs = null;

return view.extend({
	load: function() {
			return Promise.all([
				L.resolveDefault(uci.load('netquota'), null),
				L.resolveDefault(netquotad(['devices', 'list']), { devices: [] }),
				L.resolveDefault(netquotad(['status']), {}),
				L.resolveDefault(netquotad(['blocked']), { blocked: [] })
			]);
		},

		render: function(data) {
			var uciLoaded = data[0], daemonRes = data[1], status = data[2], blockedRes = data[3];
			var daemonDevs = daemonRes.devices || [];
			var blockedIps = blockedRes.blocked || [];
			var devMap = {};
			daemonDevs.forEach(function(d) { devMap[d.mac] = d; });

			var uciDevs = uciLoaded ? loadDevs() : [];
		var merged = [];
		uciDevs.forEach(function(c) {
			var s = devMap[c.mac] || {};
			merged.push({
				mac: c.mac, name: c.name || s.name || '', ip: s.ip || '',
				quota: c.quota || 0, mode: c.mode || 2, enabled: c.enabled,
				used: s.used || 0, blocked: s.blocked || false, last_seen: s.last_seen || 0,
				online: s.last_seen && (Date.now() / 1000 - s.last_seen < 300)
			});
		});
		daemonDevs.forEach(function(d) {
			if (!merged.some(function(m) { return m.mac === d.mac; })) {
				merged.push({
					mac: d.mac, name: d.name || '', ip: d.ip || '',
					quota: d.quota || 0, mode: d.mode || 2, enabled: d.enabled !== false,
					used: d.used || 0, blocked: d.blocked || false, last_seen: d.last_seen || 0,
					online: d.last_seen && (Date.now() / 1000 - d.last_seen < 300)
				});
			}
		});
		_devs = merged;

		var total = status.total_devices || merged.length;
		var online = status.online_devices || merged.filter(function(d) { return d.online; }).length;
		var blocked = status.blocked_devices || merged.filter(function(d) { return d.blocked; }).length;

		var cards = E('div', { class: 'cbi-section', style: 'display:flex;gap:1em;flex-wrap:wrap;margin:1em 0' }, [
			card('设备总数', String(total), '#2196F3'),
			card('在线设备', String(online), '#4CAF50'),
			card('已阻断', String(blocked), '#f44336')
		]);

		var btnAdd = E('button', { class: 'btn cbi-button cbi-button-add' }, '添加设备');
		var btnScan = E('button', { class: 'btn cbi-button cbi-button-action' }, '扫描局域网');
		var btnReset = E('button', { class: 'btn cbi-button cbi-button-apply' }, '今日重置');
		L.dom.event(btnAdd, 'click', function() { netquotaAddDlg(); });
		L.dom.event(btnScan, 'click', function() { netquotaScan(); });
		L.dom.event(btnReset, 'click', function() { netquotaReset(); });
		var toolbar = E('div', { style: 'margin:0.5em 0;display:flex;gap:0.5em;flex-wrap:wrap' }, [
			btnAdd, btnScan, btnReset
		]);

		_tblDiv = E('div', { id: 'netquota-tbl' });
		rebuildTable(merged);

		var tblSection = E('div', { class: 'cbi-section' }, [
			E('h3', { class: 'cbi-section-title' }, '设备管理'),
			toolbar, _tblDiv
		]);

		var settingsSection = E('div', { class: 'cbi-section' });
		settingsSection.appendChild(E('h3', { class: 'cbi-section-title' }, '全局设置'));
		if (uciLoaded) {
			var s = uci.get('netquota', 'settings');
			var rh = parseInt(s ? s.reset_hour : 0) || 0;
			var sel = E('select', { class: 'cbi-input-select' });
			for (var h = 0; h < 24; h++) {
				var opt = E('option', { value: String(h) });
				opt.textContent = (h < 10 ? '0' + h : String(h)) + ':00';
				if (h === rh) opt.selected = true;
				sel.appendChild(opt);
			}
			var btnSave = E('button', { class: 'btn cbi-button cbi-button-apply', style: 'margin-left:0.5em' }, '保存设置');
			L.dom.event(btnSave, 'click', function() {
				uci.set('netquota', 'settings', 'reset_hour', sel.value);
				uci.save('netquota');
				uci.commit('netquota').then(function() {
					ui.addNotification(null, ui.createNotification('已保存，重启 netquotad 生效', 'info'));
				});
			});
			settingsSection.appendChild(E('div', { style: 'display:flex;align-items:center;gap:0.5em;margin:0.5em 0' }, [
				E('label', { style: 'font-weight:bold' }, '重置时间：'),
				sel,
				btnSave
			]));
		}

		// 启动轮询
		poll.add(function() {
			return netquotad(['devices', 'list']).then(function(r) {
				var nd = r.devices || [];
				nd.forEach(function(d) { devMap[d.mac] = d; });
				_devs.forEach(function(dev) {
					var s = devMap[dev.mac];
					if (s) { dev.ip = s.ip || ''; dev.used = s.used || 0; dev.blocked = s.blocked || false; dev.last_seen = s.last_seen || 0; dev.online = s.last_seen && (Date.now() / 1000 - s.last_seen < 300); }
				});
				rebuildTable(_devs);
			});
		}, 15);

		return E('div', [ cards, tblSection, settingsSection ]);
	}
});

function card(label, value, color) {
	return E('div', {
		style: ['background:var(--card-bg,#fff);border:1px solid var(--border-color,#ddd);border-radius:8px;padding:1em 1.5em;min-width:120px;text-align:center;box-shadow:0 1px 3px rgba(0,0,0,0.1)'].join('')
	}, [
		E('div', { style: ['font-size:2em;font-weight:bold;color:', color].join('') }, value),
		E('div', { style: 'font-size:0.85em;color:#666;margin-top:0.3em' }, label)
	]);
}

function rebuildTable(devices) {
	if (!_tblDiv) return;
	if (!devices || devices.length === 0) {
		_tblDiv.innerHTML = '<p style="color:#999;padding:1em">暂无设备，请点击"扫描局域网"或手动添加。</p>';
		return;
	}
	var html = '<table class="table" style="width:100%"><thead><tr>' +
		'<th>设备名</th><th>MAC</th><th>IP</th><th>状态</th><th>配额</th><th>已用</th><th>剩余</th><th>操作</th>' +
		'</tr></thead><tbody>';
	devices.forEach(function(d) {
		var statusHtml, remainTxt, remainStyle = '';
		if (d.blocked) { statusHtml = '<span style="color:#f44336;font-weight:bold">● 已阻断</span>'; }
		else if (d.online) { statusHtml = '<span style="color:#4CAF50">● 在线</span>'; }
		else { statusHtml = '<span style="color:#999">○ 离线</span>'; }

		var remain = Math.max(0, d.quota - (d.used || 0));
		if (d.quota > 0) {
			remainTxt = durFmt(remain);
			if (remain <= 0) { remainStyle = 'color:#f44336;font-weight:bold'; }
		} else { remainTxt = '-'; }

		var btns = '';
		if (d.blocked) {
			btns += '<button class="btn cbi-button cbi-button-action" style="font-size:0.85em" onclick="netquotaAction(\'unblock\',\'' + d.mac + '\',\'' + (d.ip || '') + '\')">解封</button> ';
		} else if (d.quota > 0 && d.ip) {
			btns += '<button class="btn cbi-button cbi-button-negative" style="font-size:0.85em" onclick="netquotaAction(\'block\',\'' + d.mac + '\',\'' + d.ip + '\')">阻断</button> ';
		}
		btns += '<button class="btn cbi-button cbi-button-edit" style="font-size:0.85em" onclick="netquotaAction(\'delete\',\'' + d.mac + '\',\'\')">删除</button>';

		html += '<tr class="' + (d.blocked ? 'netquota-blocked' : '') + '">' +
			'<td>' + (d.name || macFmt(d.mac)) + '</td>' +
			'<td>' + macFmt(d.mac) + '</td>' +
			'<td>' + (d.ip || '-') + '</td>' +
			'<td>' + statusHtml + '</td>' +
			'<td>' + (d.quota > 0 ? durFmt(d.quota) : '无限制') + '</td>' +
			'<td>' + durFmt(d.used || 0) + '</td>' +
			'<td style="' + remainStyle + '">' + remainTxt + '</td>' +
			'<td>' + btns + '</td>' +
			'</tr>';
	});
	html += '</tbody></table>';
	_tblDiv.innerHTML = html;
}

// 全局函数（供 onclick 调用）
window.netquotaAction = function(action, mac, ip) {
	if (action === 'delete') {
		if (!confirm('确认删除设备 ' + macFmt(mac) + ' 吗？')) return;
	}
	netquotad([action, mac, ip]).then(function() {
		location.reload();
	}).catch(function(err) {
		ui.addNotification(null, ui.createNotification('操作失败: ' + (err.message || err), 'error'));
	});
};

function addDlg() {
	var dlg = new ui.Dialog('添加设备', {
		fields: [
			{ name: 'mac', label: 'MAC 地址', type: 'text', placeholder: 'AA:BB:CC:DD:EE:FF', mandatory: true },
			{ name: 'name', label: '设备名称', type: 'text', placeholder: '例如: 宝宝iPad', mandatory: true },
			{ name: 'quota', label: '每日配额(分钟)', type: 'text', placeholder: '60', mandatory: true },
			{ name: 'mode', label: '计时模式', type: 'select', choices: ['1 - 连网即计时', '2 - 有流量才计时（推荐）', '3 - 智能模式'], defaultValue: '2 - 有流量才计时（推荐）' },
			{ name: 'enabled', label: '启用', type: 'checkbox', defaultValue: true }
		]
	});
	dlg.render().then(function(ev) {
		var mm = { '1 - 连网即计时': '1', '2 - 有流量才计时（推荐）': '2', '3 - 智能模式': '3' };
		return netquotad(['devices', 'add', ev.mac.toUpperCase(), ev.name, String(parseInt(ev.quota) || 60), mm[ev.mode] || '2']);
	}).then(function() { location.reload(); }).catch(function(err) {
		ui.addNotification(null, ui.createNotification('添加失败: ' + (err.message || err), 'error'));
	});
}

function scanLAN() {
	ui.showModal('扫描局域网', '<div class="spinner"></div><p>正在扫描...</p>');
	netquotad(['scan']).then(function(res) {
		ui.hideModal();
		var scanned = res.devices || [];
		var existing = {};
		(_devs || []).forEach(function(d) { existing[d.mac] = true; });
		var news = scanned.filter(function(d) { return !existing[d.mac]; });
		if (news.length === 0) {
			ui.addNotification(null, ui.createNotification('未发现新设备', 'info'));
			return;
		}
		var html = '<p>发现 ' + news.length + ' 个新设备：</p><table class="table" style="width:100%"><tr><th>设备名</th><th>MAC</th><th>IP</th></tr>';
		news.forEach(function(d) {
			html += '<tr><td>' + (d.name || '-') + '</td><td>' + macFmt(d.mac) + '</td><td>' + (d.ip || '-') + '</td></tr>';
		});
		html += '</table><div style="margin-top:1em"><button class="btn cbi-button cbi-button-add" id="netquota-addall-btn">全部添加</button></div>';
		ui.showModal('扫描结果', html);
		setTimeout(function() {
			var btn = document.getElementById('netquota-addall-btn');
			if (btn) btn.addEventListener('click', function() {
				ui.hideModal();
				Promise.all(news.map(function(d) {
					return netquotad(['devices', 'add', d.mac, d.name || ('设备-' + d.mac.substr(0, 8)), '60', '2']);
				})).then(function() { location.reload(); }).catch(function(err) {
					ui.addNotification(null, ui.createNotification('批量添加失败: ' + (err.message || err), 'error'));
				});
			});
		}, 100);
	}).catch(function(err) {
		ui.hideModal();
		ui.addNotification(null, ui.createNotification('扫描失败: ' + (err.message || err), 'error'));
	});
}

function resetDaily() {
	netquotad(['reset']).then(function() {
		ui.addNotification(null, ui.createNotification('今日数据已重置', 'info'));
	}).catch(function(err) {
		ui.addNotification(null, ui.createNotification('重置失败: ' + (err.message || err), 'error'));
	});
}

// 全局函数（供 onclick 调用）
window.netquotaAddDlg = function() {
	var dlg = new ui.Dialog('添加设备', {
		fields: [
			{ name: 'mac', label: 'MAC 地址', type: 'text', placeholder: 'AA:BB:CC:DD:EE:FF', mandatory: true },
			{ name: 'name', label: '设备名称', type: 'text', placeholder: '例如: 宝宝iPad', mandatory: true },
			{ name: 'quota', label: '每日配额(分钟)', type: 'text', placeholder: '60', mandatory: true },
			{ name: 'mode', label: '计时模式', type: 'select', choices: ['1 - 连网即计时', '2 - 有流量才计时（推荐）', '3 - 智能模式'], defaultValue: '2 - 有流量才计时（推荐）' },
			{ name: 'enabled', label: '启用', type: 'checkbox', defaultValue: true }
		]
	});
	dlg.render().then(function(ev) {
		var mm = { '1 - 连网即计时': '1', '2 - 有流量才计时（推荐）': '2', '3 - 智能模式': '3' };
		return netquotad(['devices', 'add', ev.mac.toUpperCase(), ev.name, String(parseInt(ev.quota) || 60), mm[ev.mode] || '2']);
	}).then(function() { location.reload(); }).catch(function(err) {
		ui.addNotification(null, ui.createNotification('添加失败: ' + (err.message || err), 'error'));
	});
};

window.netquotaScan = function() {
	var existing = {};
	(_devs || []).forEach(function(d) { existing[d.mac] = true; });
	ui.showModal('扫描局域网', '<div class="spinner"></div><p>正在扫描...</p>');
	netquotad(['scan']).then(function(res) {
		ui.hideModal();
		var scanned = res.devices || [];
		var news = scanned.filter(function(d) { return !existing[d.mac]; });
		if (news.length === 0) {
			ui.addNotification(null, ui.createNotification('未发现新设备', 'info'));
			return;
		}
		var html = '<p>发现 ' + news.length + ' 个新设备：</p><table class="table" style="width:100%"><tr><th>设备名</th><th>MAC</th><th>IP</th></tr>';
		news.forEach(function(d) {
			html += '<tr><td>' + (d.name || '-') + '</td><td>' + macFmt(d.mac) + '</td><td>' + (d.ip || '-') + '</td></tr>';
		});
		html += '</table><div style="margin-top:1em"><button class="btn cbi-button cbi-button-add" id="netquota-addall-btn2">全部添加</button></div>';
		ui.showModal('扫描结果', html);
		setTimeout(function() {
			var btn = document.getElementById('netquota-addall-btn2');
			if (btn) btn.addEventListener('click', function() {
				ui.hideModal();
				Promise.all(news.map(function(d) {
					return netquotad(['devices', 'add', d.mac, d.name || ('设备-' + d.mac.substr(0, 8)), '60', '2']);
				})).then(function() { location.reload(); }).catch(function(err) {
					ui.addNotification(null, ui.createNotification('批量添加失败: ' + (err.message || err), 'error'));
				});
			});
		}, 100);
	}).catch(function(err) {
		ui.hideModal();
		ui.addNotification(null, ui.createNotification('扫描失败: ' + (err.message || err), 'error'));
	});
};

window.netquotaReset = function() {
	netquotad(['reset']).then(function() {
		ui.addNotification(null, ui.createNotification('今日数据已重置', 'info'));
	}).catch(function(err) {
		ui.addNotification(null, ui.createNotification('重置失败: ' + (err.message || err), 'error'));
	});
};

window.netquotaSaveSettings = function() {
	var sel = document.querySelector('.cbi-input-select');
	if (!sel) return;
	uci.set('netquota', 'settings', 'reset_hour', sel.value);
	uci.save('netquota');
	uci.commit('netquota').then(function() {
		ui.addNotification(null, ui.createNotification('已保存，重启 netquotad 生效', 'info'));
	});
};