// netquotad — 网络时长管理 LuCI 前端界面
'use strict';
'require view';
'require ui';
'require uci';
'require rpc';
'require poll';
'require fs';

// ====================== 工具函数 ======================

function netquotad(args) {
	return fs.exec('/usr/bin/netquotad-cli', args).then(function(res) {
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
		if (d.bypass_today) statusHtml += ' <span style="color:#FF9800;font-weight:bold">⏭ 放行中</span>';

		var remain = Math.max(0, d.quota - (d.used || 0));
		if (d.quota > 0) {
			remainTxt = durFmt(remain);
			if (remain <= 0) { remainStyle = 'color:#f44336;font-weight:bold'; }
		} else { remainTxt = '-'; }

		var btns = '';
		if (d.blocked) {
			btns += '<button class="btn cbi-button cbi-button-action" style="font-size:0.85em" onclick="window.netquotaAction(\'unblock\',\'' + d.mac + '\',\'' + (d.ip || '') + '\')">解封</button> ';
		} else if (d.quota > 0 && d.ip) {
			btns += '<button class="btn cbi-button cbi-button-negative" style="font-size:0.85em" onclick="window.netquotaAction(\'block\',\'' + d.mac + '\',\'' + d.ip + '\')">阻断</button> ';
		}
		if (d.bypass_today) {
			btns += '<button class="btn cbi-button cbi-button-apply" style="font-size:0.85em;background:#FF9800;color:#fff" onclick="window.netquotaBypass(\'' + d.mac + '\',false)">⏭ 取消放行</button> ';
		} else {
			btns += '<button class="btn cbi-button cbi-button-action" style="font-size:0.85em;background:#FF9800;color:#fff" onclick="window.netquotaBypass(\'' + d.mac + '\',true)">今日放行</button> ';
		}
		btns += '<button class="btn cbi-button cbi-button-edit" style="font-size:0.85em" onclick="window.netquotaEditDlg(\'' + d.mac + '\')">编辑</button> ';
		btns += '<button class="btn cbi-button cbi-button-edit" style="font-size:0.85em" onclick="window.netquotaAction(\'delete\',\'' + d.mac + '\',\'\')">删除</button>';

		html += '<tr class="' + (d.blocked ? 'netquota-blocked' : '') + '">' +
			'<td>' + (d.name || macFmt(d.mac)) + '</td>' +
			'<td>' + macFmt(d.mac) + '</td>' +
			'<td>' + (d.ip || '-') + '</td>' +
			'<td>' + statusHtml + '</td>' +
			'<td>' + (d.quota > 0 ? durFmt(d.quota) : '无限制') + '</td>' +
			'<td>' + durFmt(d.used || 0) + '</td>' +
			'<td style="' + remainStyle + '">' + remainTxt + '</td>' +
			'<td style="white-space:nowrap">' + btns + '</td>' +
			'</tr>';
	});
	html += '</tbody></table>';
	_tblDiv.innerHTML = html;
}

// ====================== 全局函数（供 onclick 和按钮事件调用） ======================

// 阻断/解封/删除操作
window.netquotaAction = function(action, mac, ip) {
	if (action === 'delete') {
		if (!confirm('确认删除设备 ' + macFmt(mac) + ' 吗？')) return;
		netquotad(['devices', 'delete', mac]).then(function() {
			location.reload();
		}).catch(function(err) {
			ui.addNotification(null, E('p', '操作失败: ' + (err.message || err)), 'error');
		});
	} else {
		netquotad([action, mac, ip]).then(function() {
			location.reload();
		}).catch(function(err) {
			ui.addNotification(null, E('p', '操作失败: ' + (err.message || err)), 'error');
		});
	}
};

// 添加设备对话框
window.netquotaAddDlg = function() {
	var nameInput = E('input', { type: 'text', placeholder: '例如: 宝宝iPad', style: 'width:100%;box-sizing:border-box;margin-top:5px' });
	var macInput = E('input', { type: 'text', placeholder: 'AA:BB:CC:DD:EE:FF', style: 'width:100%;box-sizing:border-box;margin-top:5px;text-transform:uppercase' });
	var quotaInput = E('input', { type: 'number', placeholder: '60', value: '60', style: 'width:100%;box-sizing:border-box;margin-top:5px' });
	var modeSelect = E('select', { style: 'width:100%;box-sizing:border-box;margin-top:5px' });
	modeSelect.innerHTML = '<option value="2">有流量才计时（推荐）</option><option value="1">连网即计时</option><option value="3">智能模式</option>';

	var content = E('div', {}, [
		E('div', { style: 'margin-bottom:8px' }, [
			E('label', { style: 'font-weight:bold' }, '设备名称 *'),
			nameInput
		]),
		E('div', { style: 'margin-bottom:8px' }, [
			E('label', { style: 'font-weight:bold' }, 'MAC 地址 *'),
			macInput
		]),
		E('div', { style: 'margin-bottom:8px' }, [
			E('label', { style: 'font-weight:bold' }, '每日配额（分钟）'),
			quotaInput
		]),
		E('div', { style: 'margin-bottom:8px' }, [
			E('label', { style: 'font-weight:bold' }, '计时模式'),
			modeSelect
		])
	]);

	ui.showModal('添加设备', [
		content,
		E('div', { style: 'text-align:right;margin-top:10px' }, [
			E('button', { class: 'btn', click: ui.hideModal }, '取消'),
			E('button', { class: 'btn cbi-button-action important', style: 'margin-left:8px',
				click: function() {
					var mac = (macInput.value || '').replace(/[^0-9A-Fa-f]/g, '').toUpperCase();
					var name = (nameInput.value || '').trim();
					var quota = parseInt(quotaInput.value) || 60;
					var mode = modeSelect.value;
					if (!mac || mac.length < 12) {
						ui.addNotification(null, E('p', '请输入有效的 MAC 地址'), 'error');
						return;
					}
					if (!name) {
						ui.addNotification(null, E('p', '请输入设备名称'), 'error');
						return;
					}
					// 格式化 MAC: AA:BB:CC:DD:EE:FF
					var fmac = '';
					for (var i = 0; i < 12; i += 2) fmac += (i > 0 ? ':' : '') + mac.substr(i, 2);
					ui.hideModal();
					netquotad(['devices', 'add', fmac, name, String(quota), mode]).then(function() {
						location.reload();
					}).catch(function(err) {
						ui.addNotification(null, E('p', '添加失败: ' + (err.message || err)), 'error');
					});
				}
			}, '确认添加')
		])
	]);
};

// 编辑设备对话框
window.netquotaEditDlg = function(mac) {
	// 从当前列表找到设备信息
	var dev = null;
	(_devs || []).forEach(function(d) { if (d.mac === mac) dev = d; });
	if (!dev) {
		ui.addNotification(null, E('p', '未找到设备信息'), 'error');
		return;
	}

	var nameInput = E('input', { type: 'text', value: dev.name || '', style: 'width:100%;box-sizing:border-box;margin-top:5px' });
	var quotaInput = E('input', { type: 'number', value: String(dev.quota || 30), style: 'width:100%;box-sizing:border-box;margin-top:5px' });
	var modeSelect = E('select', { style: 'width:100%;box-sizing:border-box;margin-top:5px' });
	modeSelect.innerHTML = '<option value="1">连网即计时</option><option value="2">有流量才计时（推荐）</option><option value="3">智能模式</option>';
	modeSelect.value = String(dev.mode || 2);

	var content = E('div', {}, [
		E('div', { style: 'margin-bottom:5px;color:#666;font-size:0.9em' }, 'MAC: ' + macFmt(mac)),
		E('div', { style: 'margin-bottom:8px' }, [
			E('label', { style: 'font-weight:bold' }, '设备名称'),
			nameInput
		]),
		E('div', { style: 'margin-bottom:8px' }, [
			E('label', { style: 'font-weight:bold' }, '每日配额（分钟）'),
			quotaInput
		]),
		E('div', { style: 'margin-bottom:8px' }, [
			E('label', { style: 'font-weight:bold' }, '计时模式'),
			modeSelect
		])
	]);

	ui.showModal('编辑设备', [
		content,
		E('div', { style: 'text-align:right;margin-top:10px' }, [
			E('button', { class: 'btn', click: ui.hideModal }, '取消'),
			E('button', { class: 'btn cbi-button-action important', style: 'margin-left:8px',
				click: function() {
					var name = (nameInput.value || '').trim();
					var quota = parseInt(quotaInput.value) || 0;
					var mode = modeSelect.value;
					if (!name) {
						ui.addNotification(null, E('p', '请输入设备名称'), 'error');
						return;
					}
					ui.hideModal();
					var json = JSON.stringify({ name: name, quota: quota, mode: parseInt(mode) });
					netquotad(['devices', 'update', mac, json]).then(function() {
						location.reload();
					}).catch(function(err) {
						ui.addNotification(null, E('p', '编辑失败: ' + (err.message || err)), 'error');
					});
				}
			}, '保存')
		])
	]);
};

// 扫描局域网
window.netquotaScan = function() {
	var existing = {};
	(_devs || []).forEach(function(d) { existing[d.mac] = true; });

	// 创建结果容器，一开始显示加载中
	var resultDiv = E('div', { style: 'text-align:center;padding:20px' }, [
		E('div', { class: 'spinner' }),
		E('p', { style: 'margin-top:10px;color:#666' }, '正在扫描局域网设备...')
	]);

	ui.showModal('扫描局域网', resultDiv);

	netquotad(['scan']).then(function(res) {
		var scanned = res.devices || [];
		var news = scanned.filter(function(d) { return !existing[d.mac]; });
		if (news.length === 0) {
			resultDiv.innerHTML = '<p style="text-align:center;padding:20px;color:#999">未发现新设备</p>';
			var closeBtn = E('button', { class: 'btn', style: 'float:right;margin-top:10px' }, '关闭');
			closeBtn.addEventListener('click', function() { ui.hideModal(); });
			resultDiv.appendChild(closeBtn);
			return;
		}

		// 构建表格
		var rowsHtml = '';
		news.forEach(function(d, idx) {
			var cbId = 'scan-cb-' + idx;
			rowsHtml += '<tr>' +
				'<td><input type="checkbox" id="' + cbId + '" checked style="transform:scale(1.2)"></td>' +
				'<td>' + (d.name || '未知设备') + '</td>' +
				'<td>' + macFmt(d.mac) + '</td>' +
				'<td>' + (d.ip || '-') + '</td>' +
				'</tr>';
		});

		var allChecked = true; // 当前全选状态

		resultDiv.innerHTML = 
			'<div style="margin-bottom:8px">' +
			'<label class="btn" style="cursor:pointer;font-size:0.85em" id="netquota-scan-toggleall">' +
			'<input type="checkbox" id="netquota-scan-togglecb" checked style="margin-right:4px">全选/取消全选' +
			'</label>' +
			'<span style="color:#999;font-size:0.85em;margin-left:8px">找到 ' + news.length + ' 个新设备</span>' +
			'</div>' +
			'<table class="table" style="width:100%"><thead><tr>' +
			'<th style="width:30px">选择</th><th>设备名</th><th>MAC</th><th>IP</th>' +
			'</tr></thead><tbody>' + rowsHtml + '</tbody></table>' +
			'<div style="text-align:right;margin-top:10px">' +
			'<button class="btn" id="netquota-scan-closebtn">关闭</button> ' +
			'<button class="btn cbi-button-action important" style="margin-left:8px" id="netquota-scan-addbtn">添加选中</button>' +
			'</div>';

		// 绑定按钮事件
		setTimeout(function() {
			// 关闭按钮
			var closeBtn = document.getElementById('netquota-scan-closebtn');
			if (closeBtn) closeBtn.addEventListener('click', function() { ui.hideModal(); });

			// 全选/取消全选
			var toggleCb = document.getElementById('netquota-scan-togglecb');
			if (toggleCb) {
				toggleCb.addEventListener('change', function() {
					var checked = toggleCb.checked;
					news.forEach(function(d, idx) {
						var cb = document.getElementById('scan-cb-' + idx);
						if (cb) cb.checked = checked;
					});
				});
			}

			// 添加选中按钮
			var addBtn = document.getElementById('netquota-scan-addbtn');
			if (addBtn) {
				addBtn.addEventListener('click', function() {
					var selected = [];
					news.forEach(function(d, idx) {
						var cb = document.getElementById('scan-cb-' + idx);
						if (cb && cb.checked) selected.push(d);
					});
					if (selected.length === 0) {
						ui.addNotification(null, E('p', '请勾选要添加的设备'), 'error');
						return;
					}
					ui.hideModal();
					setTimeout(function() {
						Promise.all(selected.map(function(d) {
							return netquotad(['devices', 'add', d.mac, d.name || ('设备-' + d.mac.substr(0, 8).replace(/:/g, '')), '60', '2']);
						})).then(function() {
							location.reload();
						}).catch(function(err) {
							ui.addNotification(null, E('p', '添加失败: ' + (err.message || err)), 'error');
						});
					}, 200);
				});
			}
		}, 100);
	}).catch(function(err) {
		resultDiv.innerHTML = '<p style="text-align:center;padding:20px;color:#f44336">扫描失败: ' + (err.message || err) + '</p>';
		var closeBtn = E('button', { class: 'btn', style: 'float:right;margin-top:10px' }, '关闭');
		closeBtn.addEventListener('click', function() { ui.hideModal(); });
		resultDiv.appendChild(closeBtn);
	});
};

// 今日重置（重置所有设备今日已用时长）
window.netquotaReset = function() {
	if (!confirm('确认重置所有设备的今日已用时长？\n此操作不可撤销！')) return;
	netquotad(['reset']).then(function() {
		ui.addNotification(null, E('p', '今日数据已重置'), 'info');
		location.reload();
	}).catch(function(err) {
		ui.addNotification(null, E('p', '重置失败: ' + (err.message || err)), 'error');
	});
};

// 今日放行/取消放行
window.netquotaBypass = function(mac, enable) {
	netquotad(['bypass', mac, enable ? 'on' : 'off']).then(function() {
		location.reload();
	}).catch(function(err) {
		ui.addNotification(null, E('p', '操作失败: ' + (err.message || err)), 'error');
	});
};

// 保存全局设置并重启 daemon
window.netquotaSaveSettings = function() {
	var sel = document.querySelector('.netquota-reset-hour');
	if (!sel) return;
	var val = sel.value;
	ui.showModal('保存设置', E('p', { style: 'text-align:center;padding:10px' }, [
		E('span', { class: 'spinner' }),
		E('span', { style: 'margin-left:8px' }, '正在保存并重启服务...')
	]));
	// 直接用 uci 命令写入配置（settings 是无名 section，用 @settings[0] 引用）
	fs.exec('/sbin/uci', ['set', 'netquota.@settings[0].reset_hour=' + val]).then(function() {
		return fs.exec('/sbin/uci', ['commit', 'netquota']);
	}).then(function() {
		return fs.exec('/etc/init.d/netquotad', ['restart']);
	}).then(function(res) {
		ui.hideModal();
		if (res.code === 0 || res.code === undefined) {
			ui.addNotification(null, E('p', '已保存为 ' + val + ':00，netquotad 已重启'), 'info');
		} else {
			ui.addNotification(null, E('p', '设置已保存，但重启 netquotad 失败: ' + (res.stderr || '')), 'error');
		}
	}).catch(function(err) {
		ui.hideModal();
		ui.addNotification(null, E('p', '保存失败: ' + (err.message || err)), 'error');
	});
};

var _tblDiv = null, _devs = null;

// ====================== 页面视图 ======================

return view.extend({
	load: function() {
		return Promise.all([
			Promise.resolve(uci.load('netquota')).catch(function() { return null; }),
			Promise.resolve(netquotad(['devices', 'list'])).catch(function() { return { devices: [] }; }),
			Promise.resolve(netquotad(['status'])).catch(function() { return {}; }),
			Promise.resolve(netquotad(['blocked'])).catch(function() { return { blocked: [] }; }),
			Promise.resolve(netquotad(['config'])).catch(function() { return {}; })
		]);
	},

	render: function(data) {
		var uciLoaded = data[0], daemonRes = data[1], status = data[2], blockedRes = data[3], daemonCfg = data[4];
		var daemonDevs = daemonRes.devices || [];
		var devMap = {};
		daemonDevs.forEach(function(d) { devMap[d.mac] = d; });

		var uciDevs = uciLoaded ? loadDevs() : [];
		var merged = [];
		uciDevs.forEach(function(c) {
			var s = devMap[c.mac] || {};
			merged.push({
				mac: c.mac, name: c.name || s.name || '', ip: s.ip || '',
				quota: c.quota || 0, mode: c.mode || 2, enabled: c.enabled,
				used: s.used || 0, blocked: s.blocked || false, bypass_today: s.bypass_today || false, last_seen: s.last_seen || 0,
				online: s.last_seen && (Date.now() / 1000 - s.last_seen < 300)
			});
		});
		daemonDevs.forEach(function(d) {
			if (!merged.some(function(m) { return m.mac === d.mac; })) {
				merged.push({
					mac: d.mac, name: d.name || '', ip: d.ip || '',
					quota: d.quota || 0, mode: d.mode || 2, enabled: d.enabled !== false,
					used: d.used || 0, blocked: d.blocked || false, bypass_today: d.bypass_today || false, last_seen: d.last_seen || 0,
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

		// 工具栏按钮 - 使用 window. 前缀确保全局函数可访问
		var btnAdd = E('button', { class: 'btn cbi-button cbi-button-add' }, '添加设备');
		btnAdd.addEventListener('click', function() { window.netquotaAddDlg(); });

		var btnScan = E('button', { class: 'btn cbi-button cbi-button-action' }, '扫描局域网');
		btnScan.addEventListener('click', function() { window.netquotaScan(); });

		var btnReset = E('button', { class: 'btn cbi-button cbi-button-apply' }, '重置今日统计');
		btnReset.title = '将所有设备的今日已用时长归零';
		btnReset.addEventListener('click', function() { window.netquotaReset(); });

		var toolbar = E('div', { style: 'margin:0.5em 0;display:flex;gap:0.5em;flex-wrap:wrap' }, [
			btnAdd, btnScan, btnReset
		]);

		_tblDiv = E('div', { id: 'netquota-tbl' });
		rebuildTable(merged);

		var tblSection = E('div', { class: 'cbi-section' }, [
			E('h3', { class: 'cbi-section-title' }, '设备管理'),
			toolbar, _tblDiv
		]);

		// 全局设置
		var settingsSection = E('div', { class: 'cbi-section' });
		settingsSection.appendChild(E('h3', { class: 'cbi-section-title' }, '每日重置时间'));
		// 从 daemon 读取配置值，避开 LuCI UCI 缓存问题
		var rh = parseInt(daemonCfg && daemonCfg.config ? daemonCfg.config.reset_hour : 0) || 0;
		var sel = E('select', { class: 'cbi-input-select netquota-reset-hour' });
		for (var h = 0; h < 24; h++) {
			var opt = E('option', { value: String(h) });
			opt.textContent = (h < 10 ? '0' + h : String(h)) + ':00';
			if (h === rh) opt.selected = true;
			sel.appendChild(opt);
		}
		var btnSave = E('button', { class: 'btn cbi-button cbi-button-apply', style: 'margin-left:0.5em' }, '保存设置');
		btnSave.addEventListener('click', function() { window.netquotaSaveSettings(); });

		settingsSection.appendChild(E('div', { style: 'display:flex;align-items:center;gap:0.5em;margin:0.5em 0;flex-wrap:wrap' }, [
			E('label', { style: 'font-weight:bold' }, '重置时间：'),
			sel,
			btnSave
		]));

		// 启动轮询（每15秒刷新状态）
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