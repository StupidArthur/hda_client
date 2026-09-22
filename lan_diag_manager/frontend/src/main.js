import './style.css'

const api = () => window.go.main.App
let selected = ''
const esc = x => String(x ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))
const fmt = x => x ? new Date(x).toLocaleString() : '—'

document.querySelector('#app').innerHTML = `<header><div><h1>局域网工具诊断中心</h1><p>按诊断地址统一查看工具运行状态</p></div><button id="poll">立即探查</button></header><nav><button class="active" data-page="status">运行状态</button><button data-page="config">工具配置</button><button data-page="history">状态历史</button></nav><main id="content"></main><div id="toast"></div>`

async function showStatus(){
 const s=await api().GetState(); const cards=s.tools.map(t=>`<article class="card ${t.status}" data-endpoint="${esc(t.endpoint)}"><div class="top"><h3>${esc(t.name)}</h3><span>${esc(t.status)}</span></div><p>${esc(t.summary)}</p><strong>${esc(t.message)}</strong><footer>${esc(t.endpoint)} · ${t.latencyMs||0} ms · ${fmt(t.checkedAt)}</footer></article>`).join('')
 document.querySelector('#content').innerHTML=`<section class="summary"><b>${s.tools.length}</b> 个已启用工具 <small>管理器 ${esc(s.version)}</small></section><section class="grid">${cards||'<div class="empty">尚未配置工具，请到“工具配置”添加。</div>'}</section><section id="detail"></section>`
 document.querySelectorAll('.card').forEach(x=>x.onclick=()=>showDetail(x.dataset.endpoint))
}
async function showDetail(endpoint){ selected=endpoint; const box=document.querySelector('#detail'); box.innerHTML='<h2>详细信息</h2><p>正在读取…</p>'; try { const d=await api().FetchDetail(endpoint); box.innerHTML=`<h2>${esc(endpoint)} 详细信息</h2><div class="detail-grid">${mapTable('info',d.info)}${mapTable('diag',d.diag)}</div>` } catch(e){ box.innerHTML=`<h2>详细信息</h2><p class="error-text">${esc(e)}</p>` }}
function mapTable(title,obj){ return `<div><h3>${title}</h3><table>${Object.keys(obj||{}).sort().map(k=>`<tr><th>${esc(k)}</th><td>${esc(obj[k])}</td></tr>`).join('')}</table></div>` }

async function showConfig(){ const s=await api().GetState(); const rows=s.config.tools.map((t,i)=>row(t,i)).join(''); document.querySelector('#content').innerHTML=`<section class="panel"><h2>工具配置</h2><div class="settings"><label>轮询秒数<input id="pollSeconds" type="number" min="2" value="${s.config.pollSeconds}"></label><label>超时秒数<input id="timeoutSeconds" type="number" min="1" value="${s.config.timeoutSeconds}"></label></div><table class="edit"><thead><tr><th>启用</th><th>名称</th><th>部署说明</th><th>IP / 主机</th><th>诊断端口</th><th></th></tr></thead><tbody id="rows">${rows}</tbody></table><div class="actions"><button id="add">添加工具</button><button class="primary" id="save">保存配置</button></div><p class="hint">配置保存在管理器 exe 同目录的 lan_diag_manager.json；状态变化历史保存在 lan_diag_history.jsonl。</p></section>`; document.querySelector('#add').onclick=()=>document.querySelector('#rows').insertAdjacentHTML('beforeend',row({enabled:true},Date.now())); document.querySelector('#save').onclick=saveConfig }
function row(t,i){return `<tr><td><input data-k="enabled" type="checkbox" ${t.enabled?'checked':''}></td><td><input data-k="name" value="${esc(t.name||'')}"></td><td><input data-k="summary" value="${esc(t.summary||'')}"></td><td><input data-k="host" value="${esc(t.host||'')}"></td><td><input data-k="port" type="number" value="${t.port||''}"></td><td><button class="remove" onclick="this.closest('tr').remove()">删除</button></td></tr>`}
async function saveConfig(){ const tools=[...document.querySelectorAll('#rows tr')].map(tr=>Object.fromEntries([...tr.querySelectorAll('[data-k]')].map(x=>[x.dataset.k,x.type==='checkbox'?x.checked:(x.dataset.k==='port'?Number(x.value):x.value)]))); try{await api().SaveConfig({pollSeconds:Number(document.querySelector('#pollSeconds').value),timeoutSeconds:Number(document.querySelector('#timeoutSeconds').value),tools}); toast('配置已保存'); showConfig()}catch(e){toast(String(e),true)} }
async function showHistory(){ const rows=await api().GetHistory(300); document.querySelector('#content').innerHTML=`<section class="panel"><h2>状态变化历史</h2><table><thead><tr><th>时间</th><th>工具</th><th>地址</th><th>变化</th><th>业务信息</th></tr></thead><tbody>${rows.map(x=>`<tr><td>${fmt(x.time)}</td><td>${esc(x.name)}</td><td>${esc(x.endpoint)}</td><td><span class="pill ${esc(x.to)}">${esc(x.from||'首次')} → ${esc(x.to)}</span></td><td>${esc(x.message)}</td></tr>`).join('')}</tbody></table></section>` }
function toast(msg,bad=false){const x=document.querySelector('#toast');x.textContent=msg;x.className=bad?'show bad':'show';setTimeout(()=>x.className='',3000)}

document.querySelector('#poll').onclick=async()=>{await api().PollNow();toast('已开始探查');setTimeout(showStatus,500)}
document.querySelectorAll('nav button').forEach(b=>b.onclick=()=>{document.querySelectorAll('nav button').forEach(x=>x.classList.remove('active'));b.classList.add('active');({status:showStatus,config:showConfig,history:showHistory})[b.dataset.page]()})
if(window.runtime) window.runtime.EventsOn('status-change',e=>{toast(`${e.name}: ${e.to} · ${e.message}`,e.to!=='ok');showStatus()})
showStatus(); setInterval(()=>document.querySelector('nav .active')?.dataset.page==='status'&&showStatus(),3000)
