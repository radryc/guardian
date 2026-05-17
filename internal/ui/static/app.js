(function(){let e=document.createElement(`link`).relList;if(e&&e.supports&&e.supports(`modulepreload`))return;for(let e of document.querySelectorAll(`link[rel="modulepreload"]`))n(e);new MutationObserver(e=>{for(let t of e)if(t.type===`childList`)for(let e of t.addedNodes)e.tagName===`LINK`&&e.rel===`modulepreload`&&n(e)}).observe(document,{childList:!0,subtree:!0});function t(e){let t={};return e.integrity&&(t.integrity=e.integrity),e.referrerPolicy&&(t.referrerPolicy=e.referrerPolicy),e.crossOrigin===`use-credentials`?t.credentials=`include`:e.crossOrigin===`anonymous`?t.credentials=`omit`:t.credentials=`same-origin`,t}function n(e){if(e.ep)return;e.ep=!0;let n=t(e);fetch(e.href,n)}})();function e(){return{activePanel:`overviewPanel`,selectedPartition:``,overview:null,detail:null,expandedAssetKey:``,expandedRolloutKeys:{},history:null,historyLoading:!1,historyError:``,rollouts:null,rolloutsLoading:!1,rolloutsError:``,catalog:null,topology:{zoom:1,nodePositions:{},selectedNodeId:``},activityDrawer:{intentName:``,data:null,loading:!1,error:``},historyOptions:{limit:10,since:``,until:``},refreshTimer:void 0}}async function t(e,t){let n=await fetch(e,{headers:{Accept:`application/json`,...t?.headers??{}},...t});if(!n.ok){let e=`HTTP ${n.status}`;try{let t=await n.json();e=t.error??t.message??e}catch{}throw Error(e)}return n.status===204?null:n.json()}function n(e,t){let n=document.getElementById(e);n&&(n.textContent=String(t))}function r(e){let t=document.getElementById(`syncIndicator`);t&&(t.textContent=e)}function i(e){return e.replace(/&/g,`&amp;`).replace(/</g,`&lt;`).replace(/>/g,`&gt;`).replace(/"/g,`&quot;`).replace(/'/g,`&#x27;`)}function a(e){return e.replace(/[^a-zA-Z0-9_-]/g,e=>`&#${e.charCodeAt(0)};`)}function o(e){return e.replace(/[-_]/g,` `).replace(/([a-z])([A-Z])/g,`$1 $2`).replace(/\b\w/g,e=>e.toUpperCase())}function s(e,t){return e.length<=t?e:`${e.slice(0,t-1)}…`}function c(e){if(!e||e===`0001-01-01T00:00:00Z`)return`—`;let t=new Date(e),n=Math.floor((Date.now()-t.getTime())/1e3);return n<60?`${n}s ago`:n<3600?`${Math.floor(n/60)}m ago`:n<86400?`${Math.floor(n/3600)}h ago`:t.toLocaleDateString(void 0,{month:`short`,day:`numeric`,year:`numeric`})}function l(e){if(!e)return``;let t=new Date(e);if(Number.isNaN(t.getTime()))return``;let n=e=>String(e).padStart(2,`0`);return`${t.getFullYear()}-${n(t.getMonth()+1)}-${n(t.getDate())}T${n(t.getHours())}:${n(t.getMinutes())}`}function u(e){let t=e.trim();if(!t)return``;let n=new Date(t);return Number.isNaN(n.getTime())?``:n.toISOString()}function d(e,t=`info`){let n=document.getElementById(`toastContainer`);if(!n)return;let r=document.createElement(`div`);r.className=`toast toast-${t}`,r.setAttribute(`role`,`status`),r.setAttribute(`aria-live`,`polite`),r.textContent=e,n.appendChild(r),requestAnimationFrame(()=>r.classList.add(`toast-visible`)),setTimeout(()=>{r.classList.remove(`toast-visible`),r.addEventListener(`transitionend`,()=>r.remove(),{once:!0}),setTimeout(()=>r.remove(),400)},3600)}var f={partition:`#F0E442`,intent:`#CC79A7`,runtime:`#0072B2`,config:`#009E73`,storage:`#56B4E9`,traffic:`#D55E00`,muted:`#8B949E`},p={healthy:`#00C369`,attention:`#FCB519`,failing:`#EE5F54`,pending:`#00ADE4`,drifted:`#FCB519`,"drifted-locked":`#F5A623`,neutral:`#566778`};function m(e){return e.kind===`partition`?200:e.kind===`intent`?230:220}function h(e){return e.kind===`partition`?76:72}function ee(e){return e.kind===`partition`?f.partition:e.kind===`intent`?f.intent:{Compute:f.runtime,Volume:f.storage,Config:f.config,ObjectStore:f.storage,Database:f.traffic,SQLDatabase:f.traffic,LoadBalancer:f.traffic,Observability:f.config}[e.assetType??``]??f.muted}function te(e){return e.kind===`partition`?`◫`:e.kind===`intent`?`⊞`:{Compute:`⧖`,Volume:`⊠`,Config:`≡`,ObjectStore:`⬜`,Database:`⫿`,SQLDatabase:`⫿`,LoadBalancer:`⊷`,Observability:`◎`}[e.assetType??``]??`⬡`}function ne(e){return e.kind===`partition`?`${e.meta?.reconciliation??`manual`} reconcile · ${e.meta?.deletionPolicy??`orphan`} delete`:e.kind===`intent`?e.meta?.target??e.displayStatus??`Intent`:`${e.assetType??`Asset`} · ${e.displayStatus??`Asset`}`}function re(e){return p[e.health??e.status??`neutral`]??p.neutral}function ie(e,t){let n=e.find(e=>e.kind===`partition`),r=e.filter(e=>e.kind===`intent`).sort((e,t)=>Number(e.level)-Number(t.level)||e.label.localeCompare(t.label)),i=e.filter(e=>e.kind===`asset`).sort((e,t)=>(e.parentID??``).localeCompare(t.parentID??``)||Number(e.level)-Number(t.level)||e.label.localeCompare(t.label)),a={},o=new Map;r.forEach(e=>{let t=Number(e.level??1);o.has(t)||o.set(t,[]),o.get(t).push(e)});let s=70;if([...o.keys()].sort((e,t)=>e-t).forEach(e=>{o.get(e).forEach(t=>{a[t.id]={x:260+(e-1)*320,y:s},s+=154}),s+=24}),n){let e=r.map(e=>a[e.id]).filter(Boolean),t=e.length?Math.min(...e.map(e=>e.y)):90,i=e.length?Math.max(...e.map(e=>e.y)):90;a[n.id]={x:40,y:Math.round((t+i)/2)}}r.forEach(e=>{let t=a[e.id];if(!t)return;let n=new Map;i.filter(t=>t.parentID===e.id).forEach(t=>{let r=Math.max(0,Number(t.level??0)-Number(e.level??0)-2);n.has(r)||n.set(r,[]),n.get(r).push(t)}),[...n.keys()].sort((e,t)=>e-t).forEach(e=>{let r=n.get(e),i=Math.max(0,(r.length-1)*96);r.forEach((n,r)=>{a[n.id]={x:t.x+280+e*250,y:t.y-i/2+r*96+e*8}})})});let c=Math.min(...Object.values(a).map(e=>e.y),40);if(c<40){let e=40-c;Object.values(a).forEach(t=>{t.y+=e})}return Object.entries(t).forEach(([e,t])=>{a[e]&&(a[e]={x:t.x,y:t.y})}),a}function ae(e,t,n,r){let i=e.x+m(n),a=e.y+h(n)/2,o=t.x,s=t.y+h(r)/2,c=o-i,l=c>=0?1:-1,u=Math.max(70,Math.abs(c)/2);return`M ${i} ${a} C ${i+u*l} ${a}, ${o-u*l} ${s}, ${o} ${s}`}function oe(e,t,n,r){e.querySelectorAll(`path.topology-edge`).forEach((e,i)=>{let a=n[i];if(!a)return;let o=t[a.from],s=t[a.to],c=r.get(a.from),l=r.get(a.to);o&&s&&c&&l&&e.setAttribute(`d`,ae(o,s,c,l))})}function g(e){let{canvas:t,topology:n,zoom:r,savedPositions:o,selectedNodeId:c,filters:l,onSelectNode:u,onDragNode:d}=e;if(!n?.nodes?.length){t.innerHTML=`<p class="empty-state" style="padding:24px">Select a partition to visualize its topology.</p>`;return}let f=n.nodes,p=new Map(f.map(e=>[e.id,e])),g=(n.edges??[]).filter(e=>l[e.kind]!==!1),_=ie(f,o),v=p.get(c)??f.find(e=>e.kind===`intent`)??f[0],y=Math.max(...Object.values(_).map(e=>e.x+260),400),b=Math.max(...Object.values(_).map(e=>e.y+100),260),x=y+40,S=b+40,C=[`<div class="topology-svg-frame">`,`<svg class="topology-svg" viewBox="0 0 ${x} ${S}" width="${Math.round(x*r)}" height="${Math.round(S*r)}" xmlns="http://www.w3.org/2000/svg">`,`<defs><filter id="ts"><feDropShadow dx="0" dy="6" stdDeviation="10" flood-color="rgba(0,0,0,0.28)"/></filter></defs>`];g.forEach(e=>{let t=_[e.from],n=_[e.to],r=p.get(e.from),o=p.get(e.to);if(!(!t||!n||!r||!o)&&(C.push(`<path class="topology-edge ${a(e.kind)}" d="${ae(t,n,r,o)}" />`),e.label)){let a=t.x+m(r),s=t.y+h(r)/2,c=n.x,l=n.y+h(o)/2;C.push(`<text class="topology-edge-label" x="${(a+c)/2}" y="${(s+l)/2-10}">${i(e.label)}</text>`)}}),f.forEach(e=>{let t=_[e.id];if(!t)return;let n=ee(e),r=v?.id===e.id,o=m(e),c=h(e);C.push(`
      <g class="topology-node ${a(e.kind)}${r?` selected`:``}" data-node="${a(e.id)}" transform="translate(${t.x},${t.y})">
        <rect class="topology-node-card" width="${o}" height="${c}" rx="12" filter="url(#ts)" />
        <rect class="topology-node-accent" width="4" height="${c}" rx="4" fill="${n}" />
        <circle cx="18" cy="18" r="5.5" fill="${re(e)}" />
        <text x="32" y="20" class="topology-node-title">${i(`${te(e)} ${e.label}`)}</text>
        <text x="32" y="38" class="topology-node-subtitle">${i(ne(e))}</text>
        <text x="14" y="60" class="topology-node-description">${i(s(e.description??``,68))}</text>
      </g>
    `)}),C.push(`</svg>`,`</div>`),t.innerHTML=C.join(``);let w=t.querySelector(`svg.topology-svg`);w.querySelectorAll(`[data-node]`).forEach(e=>{let t=e.dataset.node;function n(e,t){let n=w.createSVGPoint();n.x=e,n.y=t;let r=w.getScreenCTM();if(!r)return{x:e,y:t};let i=n.matrixTransform(r.inverse());return{x:i.x,y:i.y}}let r=!1,i=!1,a=0,o=0,s=0,c=0;e.addEventListener(`pointerdown`,n=>{if(n.button!==0)return;r=!0,i=!1,a=n.clientX,o=n.clientY;let l=_[t];s=l?l.x:0,c=l?l.y:0,e.setPointerCapture(n.pointerId),n.stopPropagation()}),e.addEventListener(`pointermove`,l=>{if(!r)return;let u=n(a,o),f=n(l.clientX,l.clientY),m=s+(f.x-u.x),h=c+(f.y-u.y);!i&&Math.abs(m-s)<3&&Math.abs(h-c)<3||(i=!0,_[t]={x:m,y:h},e.setAttribute(`transform`,`translate(${m},${h})`),e.classList.add(`dragging`),oe(w,_,g,p),d(t,{..._}),l.stopPropagation())}),e.addEventListener(`pointerup`,n=>{r&&(r=!1,e.releasePointerCapture(n.pointerId),e.classList.remove(`dragging`),i&&d(t,{..._}),n.stopPropagation())}),e.addEventListener(`click`,e=>{if(i){e.stopPropagation(),e.preventDefault();return}u(t,{..._})})})}function _(e){e&&(e.innerHTML=`
    <div class="topology-legend-group">
      <div class="topology-legend-heading">Nodes</div>
      ${[{label:`Partition`,color:f.partition},{label:`Intent`,color:f.intent},{label:`Compute`,color:f.runtime},{label:`Config`,color:f.config},{label:`Storage`,color:f.storage},{label:`Network`,color:f.traffic}].map(e=>`
        <div class="topology-legend-item">
          <span class="topology-legend-swatch" style="--legend-color:${e.color}"></span>
          <span>${i(e.label)}</span>
        </div>
      `).join(``)}
    </div>
    <div class="topology-legend-group">
      <div class="topology-legend-heading">Edges</div>
      ${[{cls:`contains`,label:`Containment`},{cls:`join`,label:`Intent join`},{cls:`dependsOn`,label:`Asset dep.`},{cls:`outputRef`,label:`Output ref`}].map(e=>`
        <div class="topology-legend-item">
          <span class="topology-edge-swatch ${a(e.cls)}"></span>
          <span>${i(e.label)}</span>
        </div>
      `).join(``)}
    </div>
  `)}var v=e();document.addEventListener(`DOMContentLoaded`,()=>{C(),Qe(),y().catch($)});async function y(){S(v.activePanel),await b(),v.refreshTimer=window.setInterval(()=>{b(v.activePanel!==`historyPanel`).catch(()=>{})},2e4)}async function b(e=!0){r(`Refreshing…`),v.overview=await t(`/api/overview`),pe(),D();let n=v.selectedPartition,i=(v.overview?.partitions??[]).map(e=>e.name);!n&&i.length>0?await x(i[0],!1):n&&i.includes(n)&&e?await x(n,!1):i.length||(v.selectedPartition=``,v.detail=null,v.history=null,v.rollouts=null,v.expandedRolloutKeys={},A(),L(),R(),V(),w()),r(`Updated just now`)}async function x(e,n=!0){if(!e)return;let r=v.selectedPartition===e;v.selectedPartition=e,v.activityDrawer={intentName:``,data:null,loading:!1,error:``},r||(v.expandedAssetKey=``,v.expandedRolloutKeys={},v.history=null,v.historyLoading=!1,v.historyError=``,v.rollouts=null,v.rolloutsLoading=!1,v.rolloutsError=``),w(),D(),v.detail=await t(`/api/partitions/${encodeURIComponent(e)}`),A(),L(),R(),V(),H(),v.activePanel===`historyPanel`&&T().catch($),v.activePanel===`rolloutsPanel`&&E(r).catch($)}function S(e){v.activePanel=e,w(),document.querySelectorAll(`.panel`).forEach(t=>{let n=t.id===e;t.classList.toggle(`active`,n),t.classList.toggle(`hidden`,!n)}),document.querySelectorAll(`[data-tab-target]`).forEach(t=>{t.classList.toggle(`active`,t.dataset.tabTarget===e)}),D(),A(),L(),R(),H(),e===`historyPanel`&&v.selectedPartition&&T().catch($),e===`rolloutsPanel`&&v.selectedPartition&&E().catch($)}function C(){let e=new URLSearchParams(window.location.search),t=e.get(`partition`);t&&(v.selectedPartition=t.trim());let n=e.get(`panel`);[`overviewPanel`,`topologyPanel`,`rolloutsPanel`,`historyPanel`].includes(n??``)&&(v.activePanel=n);let r=Number.parseInt(e.get(`historyLimit`)??``,10);Number.isFinite(r)&&r>0&&(v.historyOptions.limit=r);let i=e.get(`historySince`);i&&(v.historyOptions.since=i);let a=e.get(`historyUntil`);a&&(v.historyOptions.until=a),le()}function w(){let e=new URLSearchParams(window.location.search);v.selectedPartition?e.set(`partition`,v.selectedPartition):e.delete(`partition`),v.activePanel&&v.activePanel!==`overviewPanel`?e.set(`panel`,v.activePanel):e.delete(`panel`),v.historyOptions.limit===10?e.delete(`historyLimit`):e.set(`historyLimit`,String(v.historyOptions.limit)),v.historyOptions.since?e.set(`historySince`,v.historyOptions.since):e.delete(`historySince`),v.historyOptions.until?e.set(`historyUntil`,v.historyOptions.until):e.delete(`historyUntil`);let t=e.toString();window.history.replaceState(null,``,`${window.location.pathname}${t?`?${t}`:``}`)}async function se(e){let n=new URLSearchParams;return n.set(`limit`,String(v.historyOptions.limit)),v.historyOptions.since&&n.set(`since`,v.historyOptions.since),v.historyOptions.until&&n.set(`until`,v.historyOptions.until),t(`/api/partitions/${encodeURIComponent(e)}/history?${n.toString()}`)}async function ce(e){return t(`/api/partitions/${encodeURIComponent(e)}/rollouts`)}function le(){let e=document.getElementById(`historyLimit`);e&&(e.value=String(v.historyOptions.limit));let t=document.getElementById(`historySince`);t&&(t.value=l(v.historyOptions.since));let n=document.getElementById(`historyUntil`);n&&(n.value=l(v.historyOptions.until))}function ue(){let e=document.getElementById(`historyLimit`),t=Number.parseInt(e?.value??``,10);v.historyOptions.limit=Number.isFinite(t)&&t>0?t:10;let n=document.getElementById(`historySince`);v.historyOptions.since=u(n?.value??``);let r=document.getElementById(`historyUntil`);v.historyOptions.until=u(r?.value??``)}async function de(){if(ue(),w(),!v.selectedPartition){L();return}await T(!0)}async function fe(){v.historyOptions={limit:10,since:``,until:``},le(),await de()}async function T(e=!1){if(!v.selectedPartition){v.history=null,v.historyLoading=!1,v.historyError=``,L(),H();return}if(!v.historyLoading){if(!e&&v.history){L(),H();return}v.historyLoading=!0,v.historyError=``,L(),H();try{v.history=await se(v.selectedPartition)}catch(e){throw v.history=null,v.historyError=e?.message??`Failed to load history.`,e}finally{v.historyLoading=!1,L(),H()}}}async function E(e=!1){if(!v.selectedPartition){v.rollouts=null,v.rolloutsLoading=!1,v.rolloutsError=``,R(),H();return}if(!v.rolloutsLoading){if(!e&&v.rollouts){R(),H();return}v.rolloutsLoading=!0,v.rolloutsError=``,R(),H();try{v.rollouts=await ce(v.selectedPartition)}catch(e){throw v.rollouts=null,v.rolloutsError=e?.message??`Failed to load rollouts.`,e}finally{v.rolloutsLoading=!1,R(),H()}}}function pe(){let e=v.overview?.summary??{};n(`summaryPartitions`,e.partitions??0),n(`summaryIntents`,e.intents??0),n(`summaryAssets`,e.assets??0),n(`summaryStable`,e.healthyAssets??e.servicesHealthy??0),n(`summaryAttention`,e.attentionAssets??e.servicesAttention??0),n(`summaryFailed`,e.failingAssets??e.failedIntents??0)}function D(){let e=v.activePanel===`overviewPanel`&&!v.selectedPartition;O(),e?k():me()}function me(){let e=document.getElementById(`appGrid`);e&&(e.className=`grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5`,e.innerHTML=``)}function O(){let e=document.getElementById(`partitionList`);if(!e)return;if(!v.overview){e.className=`grid gap-1 loading-state text-sm text-[#566778]`,e.textContent=`Loading partitions…`;return}let t=document.getElementById(`partitionSearch`)?.value.trim().toLowerCase()??``,n=(v.overview?.partitions??[]).filter(e=>t?`${e.name} ${Object.keys(e.labels??{}).join(` `)} ${Object.values(e.labels??{}).join(` `)}`.toLowerCase().includes(t):!0);if(!n.length){e.className=`grid gap-1 empty-state text-sm text-[#566778]`,e.textContent=`No partitions available.`;return}e.className=`grid gap-1`,e.innerHTML=n.map(e=>`
      <button class="partition-list-item ${e.name===v.selectedPartition?`active`:``}" data-partition="${a(e.name)}">
        <div class="partition-list-title">
          <strong>${i(e.name)}</strong>
          ${U(e.health,e.displayStatus)}
        </div>
        <div class="partition-list-meta">
          <span>${e.intentCount??0} intents</span>
          <span>${e.assetCount??0} assets</span>
          <span>${e.healthyAssets??e.servicesHealthy??0} stable</span>
        </div>
      </button>
    `).join(``),e.querySelectorAll(`[data-partition]`).forEach(e=>{e.addEventListener(`click`,()=>x(e.dataset.partition).catch($))})}function k(){let e=document.getElementById(`appGrid`);if(!e)return;let t=(document.getElementById(`appGridSearch`)?.value??``).trim().toLowerCase(),n=(v.overview?.partitions??[]).filter(e=>t?`${e.name} ${Object.values(e.labels??{}).join(` `)}`.toLowerCase().includes(t):!0);if(!n.length){e.className=`grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5 empty-state text-sm text-[#566778]`,e.textContent=v.overview?`No partitions match the filter.`:`Loading partitions…`;return}e.className=`grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5`,e.innerHTML=n.map(e=>{let t=e.name===v.selectedPartition,n=Ne(e.labels??{});return`
      <button class="app-tile ${t?`active`:``}" data-partition="${a(e.name)}" data-health="${a(e.health??`neutral`)}">
        <div class="app-tile-body">
          <div class="app-tile-name">${i(e.name)}</div>
          ${n.length?`<div class="app-tile-labels">${n.map(e=>`<span class="app-tile-label">${i(e)}</span>`).join(``)}</div>`:``}
          <div class="app-tile-status-row">
            <span class="status-row">
              <span class="status-dot status-dot-${a(e.health??`neutral`)}"></span>
              <span>${i(e.displayStatus??o(e.health??`neutral`))}</span>
            </span>
          </div>
          <div class="app-tile-meta">
            <span class="app-tile-meta-item">${e.intentCount??0} intents</span>
            <span class="app-tile-meta-item">${e.assetCount??0} assets</span>
            <span class="app-tile-meta-item">${e.healthyAssets??e.servicesHealthy??0} healthy</span>
          </div>
        </div>
      </button>
    `}).join(``),e.querySelectorAll(`[data-partition]`).forEach(e=>{e.addEventListener(`click`,()=>x(e.dataset.partition).catch($))})}function A(){let e=v.detail,t=document.getElementById(`heroContent`);if(!t)return;if(v.activePanel!==`overviewPanel`){H();return}if(!e){t.className=`loading-state text-sm text-[#566778]`,t.textContent=`Select a partition to inspect its current shape.`,[`intentCards`,`attentionAssetsList`,`serviceHealthCards`,`recentEventsList`].forEach(e=>{let t=document.getElementById(e);t&&(t.className=`loading-state text-sm text-[#566778]`,t.textContent=`Choose a partition.`)}),H();return}let n=e.health??{},r={...e.partition?.manifest?.metadata?.labels??{},...e.partition?.manifest?.spec?.labels??{}};t.className=``,t.innerHTML=`
    <div class="hero-grid">
      <div class="hero-main">
        <div class="pill-row mb-2">
          ${U(n.status,n.displayStatus)}
          ${r.role?`<span class="pill">${i(r.role)}</span>`:``}
          ${r.component?`<span class="pill">${i(r.component)}</span>`:``}
          ${r.stack?`<span class="pill">${i(r.stack)}</span>`:``}
          <span class="pill">${i(e.partition.manifest.spec?.deletionPolicy??`orphan`)} deletion</span>
          <span class="pill">${i(e.partition.manifest.spec?.reconciliation?.mode??`manual`)} reconcile</span>
          ${e.compilerError?`<span class="badge badge-failing">Compiler warning</span>`:``}
        </div>
        <h2>${i(e.partition.manifest.metadata.name)}</h2>
        <p>${i(n.summary??`Partition summary unavailable.`)}</p>
        ${n.status===`pending`?Me(e):``}
        <div class="pill-row mt-2">
          ${r.endpoint?`<span class="pill">${i(r.endpoint)}</span>`:``}
          ${r.topology?`<span class="pill">${i(r.topology)}</span>`:``}
          ${r.managedBy?`<span class="pill">${i(r.managedBy)}</span>`:``}
          ${(e.partition.state?.errors??[]).map(e=>`<span class="pill">${i(e)}</span>`).join(``)}
          ${e.compilerError?`<span class="pill">${i(e.compilerError)}</span>`:``}
        </div>
      </div>
      ${j(`Healthy`,n.healthy??0)}
      ${j(`Attention`,(n.attention??0)+(n.pending??0))}
      ${j(`Failing`,n.failing??0)}
    </div>
  `,he(),N(),xe(),we(),H()}function j(e,t){return`
    <div class="stat-card rounded-lg border border-white/[0.09]">
      <div class="stat-label">${i(e)}</div>
      <div class="stat-value">${t}</div>
    </div>
  `}function he(){let e=document.getElementById(`attentionAssetsList`);if(!e)return;let t=ge(),n=_e();if(!t.length&&!n.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No assets need attention right now.`;return}e.className=`attention-asset-list`,e.innerHTML=t.map(({intent:e,asset:t})=>`
    <article class="attention-asset-card attention-asset-card-${a(t.health)}">
      <div class="attention-asset-card-header">
        <div>
          <h3>${i(t.name)}</h3>
          <div class="muted">${i(e.name)} · ${i(Z(t.type))}</div>
        </div>
        ${U(t.health,t.displayStatus)}
      </div>
      <p class="muted mt-1">${i(t.summary??``)}</p>
      <div class="pill-row mt-2">
        <span class="pill">${i(e.targetSummary??`Unassigned`)}</span>
        ${(t.quickFacts??[]).slice(0,3).map(e=>`<span class="${e.label===`Release`?`pill pill-release`:`pill`}">${i(`${e.label}: ${e.value}`)}</span>`).join(``)}
      </div>
    </article>
  `).join(``)+(n.length?`
    <div class="progressing-asset-list mt-2">
      <div class="progressing-assets-header">Progressing — awaiting first reconcile (${n.length})</div>
      ${n.map(({intent:e,asset:t})=>`
        <div class="progressing-asset-item">
          <div>
            <div>${i(t.name)}</div>
            <div class="muted">${i(e.name)} · ${i(Z(t.type))}</div>
          </div>
          ${U(t.health,t.displayStatus)}
        </div>
      `).join(``)}
    </div>
  `:``)}function ge(){return(v.detail?.intents??[]).flatMap(e=>(e.assets??[]).map(t=>({intent:e,asset:t}))).filter(({asset:e})=>e?.health===`failing`||e?.health===`attention`).sort((e,t)=>{let n=M(e.asset.health)-M(t.asset.health);return n===0?e.intent.name===t.intent.name?e.asset.name.localeCompare(t.asset.name):e.intent.name.localeCompare(t.intent.name):n})}function _e(){return(v.detail?.intents??[]).flatMap(e=>(e.assets??[]).map(t=>({intent:e,asset:t}))).filter(({asset:e})=>e?.health===`pending`).sort((e,t)=>e.intent.name===t.intent.name?e.asset.name.localeCompare(t.asset.name):e.intent.name.localeCompare(t.intent.name))}function M(e){return e===`failing`?0:e===`attention`?1:2}function N(){let e=document.getElementById(`intentCards`);if(!e)return;let t=v.detail?.intents??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No intents defined for this partition yet.`;return}e.className=`intent-stack`,e.innerHTML=t.map(e=>{let t=Xe(e.assets??[]),n=v.activityDrawer.intentName===e.name,r=Ze(e.assets??[]).map(t=>`
      <section class="intent-asset-group">
        <div class="intent-asset-group-title">
          <span class="intent-lane-group-dot" style="background:${Ye(t.category)}"></span>
          <span>${i(t.category)} · ${t.assets.length}</span>
        </div>
        <div class="asset-grid">
          ${t.assets.map(t=>{let n=P(e.name,t.name),r=v.expandedAssetKey===n,o=X(t.type),s=`asset-detail-${F(n)}`,c=[...t.quickFacts??[]].sort((e,t)=>e.label===`Release`?-1:+(t.label===`Release`)).map(e=>`<span class="fact${He(e.label)}" title="${a(Ue(e.label))}">${i(e.label)}: ${i(e.value)}</span>`).join(``),l=r?Ge(t,{limit:2**53-1,truncateAt:160}):``,u=r?Ke(t.outputs??{},[],{limit:2**53-1,truncateAt:160}):``,d=r?(t.references??[]).map(e=>`<span class="fact">${i(e)}</span>`).join(``):``,f=r&&(t.dependsOn??[]).length?(t.dependsOn??[]).map(e=>`<span class="fact">${i(e)}</span>`).join(``):``;return`
              <article
                class="asset-chip asset-chip-${a(t.health??`neutral`)}${r?` asset-chip-expanded`:``}"
                data-asset-toggle="${a(n)}"
                data-asset-card="${a(F(n))}"
                role="button"
                tabindex="0"
                aria-expanded="${r?`true`:`false`}"
                aria-controls="${a(s)}"
              >
                <div class="asset-chip-top">
                  <div>
                    <div class="asset-chip-title">${i(t.name)}</div>
                    <div class="asset-chip-type-row">
                      <span class="asset-chip-type">${i(Z(t.type))}</span>
                      <span class="asset-chip-category">${i(o)}</span>
                    </div>
                  </div>
                  ${U(t.health,t.displayStatus)}
                </div>
                ${t.summary?`<div class="muted mt-1">${i(t.summary)}</div>`:``}
                ${c?`<div class="fact-row">${c}</div>`:``}
                <div class="asset-chip-toggle-row">
                  <span class="asset-chip-toggle-copy">${r?`Hide full asset details`:`Show image, mounts, outputs, and manifest details`}</span>
                  <span class="asset-chip-toggle-indicator" aria-hidden="true">${r?`−`:`+`}</span>
                </div>
                ${r?`
                  <div class="asset-chip-details" id="${a(s)}">
                    ${f?`<div class="asset-chip-detail-block"><div class="asset-chip-detail-heading">Depends on</div><div class="fact-row">${f}</div></div>`:``}
                    ${l?`<div class="asset-chip-detail-block"><div class="asset-chip-detail-heading">Manifest details</div><div class="fact-row">${l}</div></div>`:``}
                    ${u?`<div class="asset-chip-detail-block"><div class="asset-chip-detail-heading">Outputs</div><div class="fact-row">${u}</div></div>`:``}
                    ${d?`<div class="asset-chip-detail-block"><div class="asset-chip-detail-heading">Output refs</div><div class="fact-row">${d}</div></div>`:``}
                  </div>
                `:``}
              </article>
            `}).join(``)}
        </div>
      </section>
    `).join(``);return`
      <article class="intent-card">
        <div class="intent-card-header">
          <div>
            <h3>${i(e.name)}</h3>
            <div class="muted">${i(e.summary??``)}</div>
            <div class="pill-row mt-2">
              ${U(e.health,e.displayStatus)}
              <span class="pill">${i(e.targetSummary??`Unassigned`)}</span>
              ${(e.joined??[]).map(e=>`<span class="pill">joins ${i(e)}</span>`).join(``)}
              ${t.map(e=>`<span class="pill">${i(`${e.category} ${e.count}`)}</span>`).join(``)}
              ${e.locked?`<span class="pill">locked</span>`:``}
              <button class="activity-btn ${n?`active`:``}" type="button" data-activity-intent="${a(e.name)}">&#9685;</button>
            </div>
          </div>
        </div>
        ${n?be():``}
        ${r}
      </article>
    `}).join(``),e.querySelectorAll(`[data-activity-intent]`).forEach(e=>{e.addEventListener(`click`,()=>ye(e.dataset.activityIntent).catch($))}),e.querySelectorAll(`[data-asset-toggle]`).forEach(e=>{e.addEventListener(`click`,()=>I(e.dataset.assetToggle??``)),e.addEventListener(`keydown`,t=>{t.key!==`Enter`&&t.key!==` `||(t.preventDefault(),I(e.dataset.assetToggle??``))})})}function P(e,t){return`${e}::${t}`}function F(e){return e.replace(/[^a-zA-Z0-9_-]+/g,`-`)}function I(e){e&&(v.expandedAssetKey=v.expandedAssetKey===e?``:e,N())}function ve(e){if(!e)return;v.expandedAssetKey=e,N();let t=F(e);requestAnimationFrame(()=>{document.querySelector(`[data-asset-card="${t}"]`)?.scrollIntoView({behavior:`smooth`,block:`center`,inline:`nearest`})})}async function ye(e){if(v.activityDrawer.intentName===e){v.activityDrawer={intentName:``,data:null,loading:!1,error:``},N();return}v.activityDrawer={intentName:e,data:null,loading:!0,error:``},N();try{let n=v.selectedPartition;v.activityDrawer={intentName:e,data:await t(`/api/partitions/${encodeURIComponent(n)}/intents/${encodeURIComponent(e)}/activity`),loading:!1,error:``}}catch(t){v.activityDrawer={intentName:e,data:null,loading:!1,error:t.message??`Failed to load activity`}}N()}function be(){let{data:e,loading:t,error:n}=v.activityDrawer;if(t)return`<div class="activity-drawer"><div class="activity-loading">Loading activity…</div></div>`;if(n)return`<div class="activity-drawer"><div class="activity-error">${i(n)}</div></div>`;if(!e)return`<div class="activity-drawer"><div class="activity-loading">No activity data.</div></div>`;let r=e.timestamps??{},o=[{label:`Queued`,value:r.lastQueuedAt},{label:`Check`,value:r.lastCheckAt},{label:`Diff`,value:r.lastDiffAt},{label:`Apply`,value:r.lastApplyAt}].filter(e=>e.value&&e.value!==`0001-01-01T00:00:00Z`),s=e.logs??[],l=e.drift;return`
    <div class="activity-drawer">
      <div class="activity-header">
        <span class="activity-header-title">Activity log</span>
        ${e.lastOp?`<span class="activity-op-badge">last op: ${i(e.lastOp)}</span>`:``}
        ${e.lastTaskID?`<span class="activity-task-id">${i(e.lastTaskID.slice(0,16))}…</span>`:``}
      </div>
      ${o.length?`
        <div class="activity-timestamps">
          ${o.map(e=>`<span class="activity-ts-item"><span class="activity-ts-label">${i(e.label)}</span> ${c(e.value)}</span>`).join(``)}
        </div>`:``}
      ${e.lastError?`<div class="activity-error-row"><span class="activity-error-label">Error:</span> ${i(e.lastError)}</div>`:``}
      ${l?`<div class="activity-drift">
        <span class="activity-drift-label">Drift:</span> ${i(l.summary??l.status??``)}
        ${(l.changedAssets??[]).length?`<span class="activity-drift-assets">${l.changedAssets.map(e=>i(e)).join(`, `)}</span>`:``}
      </div>`:``}
      ${s.length?`
        <div class="activity-logs-label">Logs (${s.length})</div>
        <div class="activity-logs">${s.map(e=>{let t=(e.level??`info`).toLowerCase(),n=e.asset?`[${i(e.asset)}] `:``,r=e.timestamp?c(e.timestamp)+` `:``;return`<div class="activity-log-entry ${a(t)}">${r}<span class="activity-log-level">${i(e.level??`info`)}</span> ${n}${i(e.message??``)}</div>`}).join(``)}</div>`:`<div class="activity-no-logs">No logs from last task result.</div>`}
    </div>
  `}function xe(){let e=document.getElementById(`serviceHealthCards`);if(!e)return;let t=v.detail?.health?.services??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No service-like assets to score yet.`;return}let n=t.filter(e=>e.status===`healthy`).length,r=t.filter(e=>e.status===`attention`).length,o=t.filter(e=>e.status===`failing`).length,s=t.filter(e=>e.taskActive).length,c=t.filter(e=>e.taskTimedOut).length;e.className=`service-stack`,e.innerHTML=`
    <div class="service-health-summary">
      <span class="pill">${t.length} services</span>
      ${n?`<span class="pill">stable ${n}</span>`:``}
      ${r?`<span class="pill">attention ${r}</span>`:``}
      ${o?`<span class="pill">failing ${o}</span>`:``}
      ${s?`<span class="pill">reconciling ${s}</span>`:``}
      ${c?`<span class="pill">timed out ${c}</span>`:``}
    </div>
    ${t.map(e=>`
    <article class="service-card service-card-${a(e.status??`neutral`)}">
      <div class="service-card-header">
        <div>
          <h3>${i(e.asset)}</h3>
          <div class="muted">${i(e.intent)} · ${i(Z(e.type))}</div>
        </div>
        ${U(e.status,e.displayStatus)}
      </div>
      <p class="service-card-note">${i(Se(e))}</p>
      <div class="service-health-meta">
        ${Ce(e)}
      </div>
      <div class="service-card-actions">
        <button class="btn-secondary service-card-action" type="button" data-service-focus="${a(P(e.intent,e.asset))}">Open details</button>
      </div>
    </article>
  `).join(``)}
  `,e.querySelectorAll(`[data-service-focus]`).forEach(e=>{e.addEventListener(`click`,()=>ve(e.dataset.serviceFocus??``))})}function Se(e){if(e.taskTimedOut)return`Last reconcile task timed out. Open details in the intent map for config and outputs.`;if(e.taskActive)return`Reconcile is currently running for this service.`;switch(e.status){case`healthy`:return`Operational summary only. Configuration and ports stay in the intent map.`;case`pending`:return`Waiting for the first successful reconcile.`;case`attention`:return String(e.summary??`Needs attention.`);case`failing`:return String(e.summary??`Service is failing.`);default:return String(e.summary??`Service status unavailable.`)}}function Ce(e){let t=[];e.taskActive&&t.push(`reconcile running`),e.taskTimedOut&&t.push(`last task timed out`);let n=c(e.lastUpdatedAt);return n!==`—`&&t.push(`updated ${n}`),t.map(e=>`<span class="service-health-meta-item">${i(e)}</span>`).join(``)}function we(){let e=document.getElementById(`recentEventsList`);if(!e)return;let t=v.detail?.recentEvents??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Recent event history loads only in the History tab.`;return}e.className=`timeline-stack`,e.innerHTML=z(t).map(B).join(``)}function L(){let e=document.getElementById(`deploymentTimeline`),t=document.getElementById(`eventTimeline`);if(!e||!t)return;if(!v.selectedPartition){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a partition to inspect deployment history.`,t.className=`empty-state text-sm text-[#566778]`,t.textContent=`Select a partition to inspect event history.`;return}if(v.historyLoading){e.className=`loading-state text-sm text-[#566778]`,e.textContent=`Loading deployment history…`,t.className=`loading-state text-sm text-[#566778]`,t.textContent=`Loading event history…`;return}if(v.historyError){e.className=`empty-state text-sm text-[#566778]`,e.textContent=v.historyError,t.className=`empty-state text-sm text-[#566778]`,t.textContent=v.historyError;return}let n=v.history;if(!n){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Open the History tab to load deployment history.`,t.className=`empty-state text-sm text-[#566778]`,t.textContent=`Open the History tab to load event history.`;return}let r=(document.getElementById(`historyFilter`)?.value??``).trim().toLowerCase(),i=(n.deployments??[]).filter(e=>r?`${e.intent} ${e.deploymentRevision} ${(e.assets??[]).map(e=>`${e.asset} ${e.version??``}`).join(` `)}`.toLowerCase().includes(r):!0),a=(n.events??[]).filter(e=>r?`${e.intent??``} ${e.title??``} ${e.message??``}`.toLowerCase().includes(r):!0);e.className=i.length?`timeline-stack`:`empty-state text-sm text-[#566778]`,e.innerHTML=i.length?i.map(ke).join(``):`No deployment entries match the current filter.`;let o=document.getElementById(`historyGroupToggle`),s=!o||o.checked?z(a):a;t.className=s.length?`timeline-stack`:`empty-state text-sm text-[#566778]`,t.innerHTML=s.length?s.map(B).join(``):`No events match the current filter.`}function R(){let e=document.getElementById(`rolloutsTimeline`);if(!e)return;if(!v.selectedPartition){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a partition to inspect rollout history.`;return}if(v.rolloutsLoading){e.className=`loading-state text-sm text-[#566778]`,e.textContent=`Loading rollouts…`;return}if(v.rolloutsError){e.className=`empty-state text-sm text-[#566778]`,e.textContent=v.rolloutsError;return}let t=v.rollouts?.rollouts??[];e.className=t.length?`timeline-stack`:`empty-state text-sm text-[#566778]`,e.innerHTML=t.length?t.map(Te).join(``):`No archived rollouts were found for this partition yet.`,e.querySelectorAll(`[data-rollout-toggle]`).forEach(e=>{e.addEventListener(`click`,t=>{t.preventDefault();let n=e.dataset.rolloutToggle??``;n&&Ee(n)})})}function Te(e){let t=e.assets??[],n=t.length,r=De(e),s=!!v.expandedRolloutKeys[r],l=e.current?U(`healthy`,`Current`):e.newIntent?U(`pending`,`New intent`):U(`healthy`,`Rollout`);return`
    <article class="timeline-card">
      <div class="timeline-head">
        <div>
          <h3>${i(e.intent)}</h3>
          <div class="muted">${i(e.summary||e.deploymentRevision)}</div>
        </div>
        <div class="timeline-head-actions">
          ${n?`
            <button
              class="rollout-toggle${s?` active`:``}"
              type="button"
              data-rollout-toggle="${a(r)}"
              aria-expanded="${s?`true`:`false`}"
              aria-label="${s?`Hide asset details`:`Show asset details`}"
            >
              <span class="rollout-toggle-indicator" aria-hidden="true">${s?`−`:`+`}</span>
              <span>${s?`Hide assets`:`Assets ${n}`}</span>
            </button>
          `:``}
          ${l}
        </div>
      </div>
      <div class="timeline-meta">
        <span>${c(e.createdAt)}</span>
        ${e.target?`<span>${i(e.target)}</span>`:``}
        <span>${i(e.deploymentRevision)}</span>
        ${(e.taskIDs??[]).map(e=>`<span>${i(e)}</span>`).join(``)}
      </div>
      ${n?`<div class="timeline-assets-summary muted">${s?`${n} asset${n===1?``:`s`} shown`:`${n} asset${n===1?``:`s`} hidden`}</div>`:``}
      ${s?`
        <div class="timeline-assets">
          ${t.map(t=>`
            <div class="timeline-asset">
              <div class="flex justify-between items-start gap-2">
                <div>
                  <strong class="text-[13px] text-[#E5ECF4]">${i(t.name)}</strong>
                  <div class="muted">${i(t.type||`Asset`)}</div>
                </div>
                ${U(Oe(t.change),o(t.change||`updated`))}
              </div>
              <div class="fact-row mt-1">
                <span class="fact fact-release">Release: ${i(t.version||e.deploymentRevision)}</span>
                ${t.type?`<span class="fact">Type: ${i(t.type)}</span>`:``}
              </div>
            </div>
          `).join(``)}
        </div>
      `:``}
    </article>
  `}function Ee(e){e&&(v.expandedRolloutKeys={...v.expandedRolloutKeys,[e]:!v.expandedRolloutKeys[e]},R())}function De(e){return`${e.intent??``}::${e.deploymentRevision??``}`}function Oe(e){switch((e??``).toLowerCase()){case`added`:return`pending`;case`removed`:return`attention`;default:return`healthy`}}function z(e){let t=new Map;for(let n of e){let e=`${n.intent??``}::${n.title}`,r=t.get(e);!r||new Date(n.timestamp)>new Date(r.latest.timestamp)?t.set(e,{latest:n,count:(r?.count??0)+1}):r.count++}return Array.from(t.values()).sort((e,t)=>new Date(t.latest.timestamp).getTime()-new Date(e.latest.timestamp).getTime()).map(e=>({...e.latest,groupCount:e.count}))}function B(e){let t=e.groupCount>1?`<span class="event-count-pill" title="${e.groupCount} occurrences">${e.groupCount}×</span>`:``,n=(e.title??``).toLowerCase().replace(/[^a-z0-9]/g,``),r=(e.message??``).toLowerCase().replace(/[^a-z0-9]/g,``),a=e.message&&r!==n;return`
    <article class="timeline-card">
      <div class="timeline-head">
        <div>
          <span class="event-type-eyebrow">Event type</span>
          <h3>${i(e.title??`Event`)} ${t}</h3>
          ${a?`<div class="muted">${i(e.message)}</div>`:``}
        </div>
        ${U(e.status,e.displayStatus)}
      </div>
      <div class="timeline-meta">
        <span>${c(e.timestamp)}</span>
        ${e.intent?`<span>${i(e.intent)}</span>`:``}
        ${e.taskID?`<span>${i(e.taskID)}</span>`:``}
        ${e.deploymentRevision?`<span>${i(e.deploymentRevision)}</span>`:``}
      </div>
    </article>
  `}function ke(e){return`
    <article class="timeline-card">
      <div class="timeline-head">
        <div>
          <h3>${i(e.intent)}</h3>
          <div class="muted">${i(e.deploymentRevision)}</div>
        </div>
        <span class="badge badge-healthy">Pushed</span>
      </div>
      <div class="timeline-meta">
        <span>${c(e.createdAt)}</span>
        <span>${i(e.target??`Unassigned`)}</span>
        ${(e.taskIDs??[]).map(e=>`<span>${i(e)}</span>`).join(``)}
      </div>
      <div class="timeline-assets">
        ${(e.assets??[]).map(e=>`
          <div class="timeline-asset">
            <div class="flex justify-between items-start gap-2">
              <div>
                <strong class="text-[13px] text-[#E5ECF4]">${i(e.asset)}</strong>
                <div class="muted">${i(e.summary??``)}</div>
              </div>
              ${U(e.status,e.displayStatus)}
            </div>
            <div class="fact-row mt-1">
              ${e.version?`<span class="fact fact-release">Release: ${i(e.version)}</span>`:``}
              ${Object.entries(e.outputs??{}).map(([e,t])=>`<span class="fact">${i(e)}=${i(String(t))}</span>`).join(``)}
            </div>
            <div class="timeline-asset-logs">
              ${(e.logs??[]).map(e=>`<div class="timeline-log">${i(e.level??`info`)} · ${i(e.message??``)}</div>`).join(``)}
            </div>
          </div>
        `).join(``)}
      </div>
    </article>
  `}function V(){let e=document.getElementById(`topologyCanvas`);if(!e)return;let t=v.detail?.topology;_(document.getElementById(`topologyLegend`)),g({canvas:e,topology:t,zoom:v.topology.zoom,savedPositions:v.topology.nodePositions,selectedNodeId:v.topology.selectedNodeId,filters:{contains:document.getElementById(`showContainEdges`)?.checked??!0,join:document.getElementById(`showJoinEdges`)?.checked??!0,dependsOn:document.getElementById(`showAssetEdges`)?.checked??!0,outputRef:document.getElementById(`showOutputEdges`)?.checked??!0},onSelectNode:(e,t)=>{v.topology.selectedNodeId=e,v.topology.nodePositions=t,Ae(),V()},onDragNode:(e,t)=>{v.topology.nodePositions=t}}),Ae()}function Ae(){let e=document.getElementById(`topologyDetails`);if(!e)return;let t=v.detail?.topology;if(!t?.nodes?.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a node to inspect its status, metadata, and linked details.`;return}let n=new Map(t.nodes.map(e=>[e.id,e])),r=n.get(v.topology.selectedNodeId);if(!r){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a node to inspect its status, metadata, and linked details.`;return}let a={contains:document.getElementById(`showContainEdges`)?.checked??!0,join:document.getElementById(`showJoinEdges`)?.checked??!0,dependsOn:document.getElementById(`showAssetEdges`)?.checked??!0,outputRef:document.getElementById(`showOutputEdges`)?.checked??!0},s=(t.edges??[]).filter(e=>a[e.kind]!==!1).filter(e=>e.from===r.id||e.to===r.id),c=r.kind===`asset`?Fe(r.intent,r.asset??r.label):null,l=r.kind===`intent`?Pe(r.intent??r.label):null,u=c?Ge(c):``,d=Ke(c?.outputs??l?.outputs??{},c?[]:l?.outputHints??[]),f=(c?.references??[]).map(e=>`<span class="fact">${i(e)}</span>`).join(``),p=s.map(e=>{let t=e.from===r.id?e.to:e.from,a=n.get(t);return a?`
      <div class="topology-detail-row">
        <span class="topology-detail-direction">${e.from===r.id?`out`:`in`}</span>
        <span class="topology-detail-name">${i(a.label)}</span>
        <span class="topology-detail-kind">${i(o(e.kind))}</span>
      </div>
    `:``}).join(``);e.className=`topology-detail-card`,e.innerHTML=`
    <div style="--node-accent:${Ie(r)}">
      <div class="topology-detail-header">
        <div class="topology-detail-icon">${i(Le(r))}</div>
        <div>
          <h3>${i(r.label)}</h3>
          <p>${i(Re(r))}</p>
        </div>
      </div>
      <div class="pill-row mb-2">
        ${U(r.health??r.status,r.displayStatus??o(r.kind))}
        <span class="pill">${i(o(r.kind))}</span>
        ${r.assetType?`<span class="pill">${i(Z(r.assetType))}</span>`:``}
      </div>
      <div class="topology-detail-copy">${i(r.description??``)}</div>
      ${Object.keys(r.meta??{}).length?`
        <div class="topology-detail-meta mt-2">
          ${Object.entries(r.meta??{}).map(([e,t])=>`<span class="fact">${i(`${o(e)}: ${t}`)}</span>`).join(``)}
        </div>
      `:``}
      ${u?`<div class="topology-detail-block"><div class="topology-detail-heading">Properties</div><div class="fact-row">${u}</div></div>`:``}
      ${d?`<div class="topology-detail-block"><div class="topology-detail-heading">Outputs</div><div class="fact-row">${d}</div></div>`:``}
      ${f?`<div class="topology-detail-block"><div class="topology-detail-heading">Output refs</div><div class="fact-row">${f}</div></div>`:``}
      <div class="topology-detail-block">
        <div class="topology-detail-heading">Linked edges</div>
        ${p?`<div class="topology-detail-list">${p}</div>`:`<div class="muted">No linked edges after current filters.</div>`}
      </div>
    </div>
  `}var je={overviewPanel:{eyebrow:`Control center`,title:`Control Center`},topologyPanel:{eyebrow:`Deployment graph`,title:`Topology`},rolloutsPanel:{eyebrow:`Release timeline`,title:`Rollouts`},historyPanel:{eyebrow:`Push timeline`,title:`History`}};function H(){let e=v.activePanel,t=v.detail,r=je[e]??je.overviewPanel,a=e===`overviewPanel`,o=!!v.selectedPartition,s=a&&!o,c=(e,t)=>{let n=document.getElementById(e);n&&(n.style.display=t?``:`none`)};c(`appGridSection`,s),c(`summaryGrid`,s),c(`selectedPartitionHero`,a&&o),c(`sidebarPartitionSection`,!0),n(`pageEyebrow`,r.eyebrow),n(`pageTitle`,r.title);let l=`Monitor partitions, inspect topology, and review history.`,u=``;e===`overviewPanel`&&t&&(l=`${t.partition.manifest.spec?.deletionPolicy??`orphan`} policy · ${t.partition.manifest.spec?.reconciliation?.mode??`manual`} reconcile · ${t.intents.length} intents`,u=`${U(t.health?.status,t.health?.displayStatus??`Selected`)} <span class="pill">${t.topology?.nodes?.length??0} nodes</span>`),e===`topologyPanel`&&(l=t?`Topology for ${t.partition.manifest.metadata.name}.`:`Select a partition to inspect its graph.`,u=t?`<span class="pill">${i(t.partition.manifest.metadata.name)}</span><span class="pill">${t.topology?.nodes?.length??0} nodes</span>`:``),e===`rolloutsPanel`&&(l=t?`Review archived rollout changes for ${t.partition.manifest.metadata.name}.`:`Select a partition to inspect rollout history.`,u=t?`<span class="pill">${i(t.partition.manifest.metadata.name)}</span><span class="pill">${v.rollouts?.rollouts?.length??0} rollouts</span>`:``),e===`historyPanel`&&(l=t?`Review deployments and state changes for ${t.partition.manifest.metadata.name}.`:`Select a partition to inspect history.`,u=t?`<span class="pill">${i(t.partition.manifest.metadata.name)}</span><span class="pill">${v.history?.deployments?.length??0} deployments</span>`:``),n(`pageSubtitle`,l);let d=document.getElementById(`headerContextPills`);d&&(d.innerHTML=u,d.style.display=u.trim()?``:`none`);let f=document.getElementById(`topnavPartition`);f&&(f.textContent=v.selectedPartition||t?.partition?.manifest?.metadata?.name||r.title||`Control Center`)}function U(e,t){return`<span class="badge badge-${a(e??`neutral`)}">${i(t??o(e??`neutral`))}</span>`}function Me(e){let t=e.partition?.manifest?.spec?.reconciliation?.mode??`manual`,n=e.partition?.manifest?.spec?.labels?.managedBy??``,r=(e.intents??[]).some(e=>e.targetSummary&&e.targetSummary!==`Unassigned`),i=e.health?.pending??0,a=[];return a.push(`${i} asset${i===1?` is`:`s are`} in <strong>Planned</strong> state — no reconcile has run yet.`),n===`external-compose`&&a.push(`Resources in this partition are managed externally by Docker Compose.`),t===`manual`?r?a.push(`Click <strong>Reconcile now</strong> in the sidebar to run the first reconcile and deploy assets.`):a.push(`No pusher is assigned. Assets will stay in Planned state until a pusher is configured.`):a.push(`The reconciler will process these assets automatically in the next cycle.`),`<div class="info-callout mt-2"><span class="info-callout-icon">?</span><div><strong>Why is this partition Progressing?</strong><p>${a.join(` `)}</p></div></div>`}function Ne(e){let t=[`component`,`role`,`stack`,`topology`],n=[];return t.forEach(t=>{e[t]&&n.push(e[t])}),[...new Set(n)]}function Pe(e){return(v.detail?.intents??[]).find(t=>t.name===e)??null}function Fe(e,t){return Pe(e)?.assets?.find(e=>e.name===t)??null}var W={partition:`#F0E442`,intent:`#CC79A7`,runtime:`#0072B2`,config:`#009E73`,storage:`#56B4E9`,traffic:`#D55E00`,muted:`#8B949E`};function Ie(e){return e.kind===`partition`?W.partition:e.kind===`intent`?W.intent:Je(e.assetType??e.kind)}function Le(e){return e.kind===`partition`?`◫`:e.kind===`intent`?`⊞`:qe(e.assetType??e.kind)}function Re(e){return e.kind===`partition`?`${e.meta?.reconciliation??`manual`} reconcile · ${e.meta?.deletionPolicy??`orphan`} delete`:e.kind===`intent`?e.meta?.target??e.displayStatus??`Intent`:`${Z(e.assetType??e.kind)} · ${e.displayStatus??`Asset`}`}var G=[`Compute`,`Network`,`Config`,`Storage`,`Operations`],ze={Compute:W.runtime,Volume:W.storage,Config:W.config,ObjectStore:W.storage,Database:W.traffic,SQLDatabase:W.traffic,LoadBalancer:W.traffic,Observability:W.config},Be={Image:`Container image reference (registry/name:tag@digest)`,Scale:`Desired replica count`,Env:`Environment variables injected at runtime`,Config:`ConfigMap or file mounts`,Storage:`Persistent volume mounts`,Ports:`Exposed service ports`,Port:`Service listener port`,Health:`Health check probe is configured`,CPU:`CPU resource limit/request`,Memory:`Memory resource limit/request`,Engine:`Storage engine or database type`,Version:`Engine version`,Database:`Database name`,Mode:`Deployment or storage mode`,Endpoint:`Connection endpoint address`,Size:`Volume storage capacity`,Access:`Volume access mode (e.g. ReadWriteOnce)`,Format:`Config file format (yaml / json / env)`,Files:`Config file definitions`,Inline:`Config data is defined inline in the manifest`,Targets:`Number of load balancer backend targets`,Listeners:`Number of load balancer listeners`,Buckets:`Object storage bucket names`,Provider:`Observability provider`,Receivers:`Telemetry input protocols`,Exporters:`Telemetry export destinations`,Outputs:`Output keys exposed to dependent intents`},Ve={Scale:`fact-scale`,Ports:`fact-port`,Port:`fact-port`,CPU:`fact-resource`,Memory:`fact-resource`,Env:`fact-env`,Storage:`fact-storage`,Size:`fact-storage`,Engine:`fact-engine`,Version:`fact-engine`,Outputs:`fact-outputs`};function He(e){return e===`Release`?`fact-release`:Ve[e]?` ${Ve[e]}`:``}function Ue(e){return Be[e]??e}function K(e){return(v.catalog?.assetTypes??[]).find(t=>t.type===e)??null}function q(e){return e.replace(/\[\d+\]/g,`[]`)}function J(e,t){let n=q(t),r=n.replace(/\[\]/g,``).split(`.`)[0];return(e??[]).find(e=>e.path===n||e.path===r)??null}function We(e,t){let n=K(e?.type??``);return J(e?.hints,t)??J(n?.hints,t)??J(n?.fields,t)??null}function Y(e,t=``){if(e==null||e===``)return[];if(Array.isArray(e))return e.length?e.flatMap((e,n)=>Y(e,`${t}[${n}]`)):t?[{path:t,value:`[]`}]:[];if(typeof e==`object`){let n=Object.entries(e);return n.length?n.flatMap(([e,n])=>Y(n,t?`${t}.${e}`:e)):t?[{path:t,value:`{}`}]:[]}return t?[{path:t,value:String(e)}]:[]}function Ge(e,t={}){let n=t.limit??16,r=t.truncateAt??48,o=K(e?.type??``),c=new Map;(o?.fields??[]).forEach((e,t)=>{c.set(e.path,t)});let l=Y(e?.properties??{}).sort((e,t)=>{let n=q(e.path),r=q(t.path),i=n.replace(/\[\]/g,``).split(`.`)[0],a=r.replace(/\[\]/g,``).split(`.`)[0],o=c.get(n)??c.get(i)??2**53-1,s=c.get(r)??c.get(a)??2**53-1;return o===s?e.path.localeCompare(t.path):o-s});if(!l.length)return``;let u=l.slice(0,n),d=u.map(t=>{let n=We(e,t.path);return`<span class="fact" title="${a([t.path,n?.title,n?.description].filter(Boolean).join(` - `))}">${i(`${t.path}: ${s(t.value,r)}`)}</span>`}).join(``);if(l.length===u.length)return d;let f=l.length-u.length;return`${d}<span class="fact" title="${f} more properties available">+${f} more</span>`}function Ke(e,t=[],n={}){let r=Object.entries(e??{}),o=n.limit??r.length,c=n.truncateAt??2**53-1,l=r.slice(0,o).map(([e,n])=>{let r=J(t,`outputs.${e}`);return`<span class="fact" title="${a([`outputs.${e}`,r?.title,r?.description].filter(Boolean).join(` - `))}">${i(`${e}: ${s(String(n),c)}`)}</span>`}).join(``);if(r.length<=o)return l;let u=r.length-o;return`${l}<span class="fact" title="${u} more outputs available">+${u} more</span>`}function X(e){return K(e)?.category??`Other`}function Z(e){return K(e)?.title??o(e)}function qe(e){return K(e)?.icon??`⬡`}function Je(e){return ze[e]??W.muted}function Ye(e){let t={Compute:W.runtime,Network:W.traffic,Config:W.config,Storage:W.storage,Operations:W.config,Other:W.muted};return t[e]??t.Other}function Xe(e){let t=new Map;return e.forEach(e=>{let n=X(e.type);t.set(n,(t.get(n)??0)+1)}),[...t.keys()].sort((e,t)=>{let n=G.indexOf(e),r=G.indexOf(t);return n===-1&&r===-1?e.localeCompare(t):n===-1?1:r===-1?-1:n-r}).map(e=>({category:e,count:t.get(e)}))}function Ze(e){let t=new Map;return e.forEach(e=>{let n=X(e.type);t.has(n)||t.set(n,[]),t.get(n).push(e)}),[...t.keys()].sort((e,t)=>{let n=G.indexOf(e),r=G.indexOf(t);return n===-1&&r===-1?e.localeCompare(t):n===-1?1:r===-1?-1:n-r}).map(e=>({category:e,assets:t.get(e).sort((e,t)=>e.name.localeCompare(t.name))}))}function Qe(){document.querySelectorAll(`[data-tab-target]`).forEach(e=>{e.addEventListener(`click`,()=>S(e.dataset.tabTarget))}),document.getElementById(`partitionSearch`)?.addEventListener(`input`,O),document.getElementById(`refreshButton`)?.addEventListener(`click`,()=>b(!0).catch($)),document.getElementById(`reconcileButton`)?.addEventListener(`click`,$e),document.getElementById(`createPartitionButton`)?.addEventListener(`click`,()=>d(`Create partition via guardianctl partition push`,`success`)),document.getElementById(`overviewCreatePartitionButton`)?.addEventListener(`click`,()=>d(`Create partition via guardianctl partition push`,`success`)),document.getElementById(`appGridSearch`)?.addEventListener(`input`,k),document.getElementById(`historyFilter`)?.addEventListener(`input`,L),document.getElementById(`historyGroupToggle`)?.addEventListener(`change`,L),document.getElementById(`historyApply`)?.addEventListener(`click`,()=>de().catch($)),document.getElementById(`historyReset`)?.addEventListener(`click`,()=>fe().catch($)),[`showContainEdges`,`showJoinEdges`,`showAssetEdges`,`showOutputEdges`].forEach(e=>{document.getElementById(e)?.addEventListener(`change`,V)}),document.getElementById(`topologyZoomOut`)?.addEventListener(`click`,()=>et(-.1)),document.getElementById(`topologyZoomIn`)?.addEventListener(`click`,()=>et(.1)),document.getElementById(`topologyResetLayout`)?.addEventListener(`click`,()=>{v.topology.nodePositions={},V()})}async function $e(){let e=v.selectedPartition;if(!e){d(`Select a partition first.`,`error`);return}await t(`/api/partitions/${encodeURIComponent(e)}/reconcile`,{method:`POST`}),d(`Reconciliation requested.`,`success`),await b(!1),await x(e,!1)}function et(e){let t=document.getElementById(`topologyCanvas`),n=Q(v.topology.zoom,.4,2.5),r=Q(Math.round((n+e)*100)/100,.4,2.5);if(n===r)return;let i=t?t.scrollLeft+t.clientWidth/2:0,a=t?t.scrollTop+t.clientHeight/2:0;if(v.topology.zoom=r,V(),t){let e=r/n;t.scrollLeft=Math.max(0,i*e-t.clientWidth/2),t.scrollTop=Math.max(0,a*e-t.clientHeight/2)}tt()}function tt(){let e=v.topology.zoom,t=document.getElementById(`topologyZoomOut`),n=document.getElementById(`topologyZoomIn`),r=document.getElementById(`topologyZoomValue`);r&&(r.textContent=`${Math.round(e*100)}%`),t&&(t.disabled=e<=.4),n&&(n.disabled=e>=2.5)}function Q(e,t,n){return Math.min(n,Math.max(t,e))}function $(e){d(e?.message??`Unexpected error`,`error`)}async function nt(){try{v.catalog=await t(`/api/catalog`)}catch{}}nt();