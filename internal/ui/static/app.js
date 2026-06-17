(function(){let e=document.createElement(`link`).relList;if(e&&e.supports&&e.supports(`modulepreload`))return;for(let e of document.querySelectorAll(`link[rel="modulepreload"]`))n(e);new MutationObserver(e=>{for(let t of e)if(t.type===`childList`)for(let e of t.addedNodes)e.tagName===`LINK`&&e.rel===`modulepreload`&&n(e)}).observe(document,{childList:!0,subtree:!0});function t(e){let t={};return e.integrity&&(t.integrity=e.integrity),e.referrerPolicy&&(t.referrerPolicy=e.referrerPolicy),e.crossOrigin===`use-credentials`?t.credentials=`include`:e.crossOrigin===`anonymous`?t.credentials=`omit`:t.credentials=`same-origin`,t}function n(e){if(e.ep)return;e.ep=!0;let n=t(e);fetch(e.href,n)}})();function e(){return{activePanel:`overviewPanel`,selectedPartition:``,overview:null,detail:null,expandedAssetKey:``,expandedRolloutKeys:{},history:null,historyLoading:!1,historyError:``,rollouts:null,rolloutsLoading:!1,rolloutsError:``,catalog:null,topology:{zoom:1,nodePositions:{},selectedNodeId:``},activityDrawer:{intentName:``,data:null,loading:!1,error:``},historyOptions:{limit:10,since:``,until:``},refreshTimer:void 0,rolloutsRefreshTimer:void 0,fastRefreshUntil:0,diagnosticDetails:{}}}async function t(e,t){let n=await fetch(e,{headers:{Accept:`application/json`,...t?.headers??{}},...t});if(!n.ok){let e=`HTTP ${n.status}`;try{let t=await n.json();e=t.error??t.message??e}catch{}throw Error(e)}return n.status===204?null:n.json()}function n(e,t){let n=document.getElementById(e);n&&(n.textContent=String(t))}function r(e){let t=document.getElementById(`syncIndicator`);t&&(t.textContent=e)}function i(e){return e.replace(/&/g,`&amp;`).replace(/</g,`&lt;`).replace(/>/g,`&gt;`).replace(/"/g,`&quot;`).replace(/'/g,`&#x27;`)}function a(e){return e.replace(/[^a-zA-Z0-9_-]/g,e=>`&#${e.charCodeAt(0)};`)}function o(e){return e.replace(/[-_]/g,` `).replace(/([a-z])([A-Z])/g,`$1 $2`).replace(/\b\w/g,e=>e.toUpperCase())}function s(e,t){return e.length<=t?e:`${e.slice(0,t-1)}…`}function c(e){if(!e||e===`0001-01-01T00:00:00Z`)return`—`;let t=new Date(e),n=Math.floor((Date.now()-t.getTime())/1e3);return n<60?`${n}s ago`:n<3600?`${Math.floor(n/60)}m ago`:n<86400?`${Math.floor(n/3600)}h ago`:t.toLocaleDateString(void 0,{month:`short`,day:`numeric`,year:`numeric`})}function l(e){if(!e)return``;let t=new Date(e);if(Number.isNaN(t.getTime()))return``;let n=e=>String(e).padStart(2,`0`);return`${t.getFullYear()}-${n(t.getMonth()+1)}-${n(t.getDate())}T${n(t.getHours())}:${n(t.getMinutes())}`}function u(e){let t=e.trim();if(!t)return``;let n=new Date(t);return Number.isNaN(n.getTime())?``:n.toISOString()}function d(e,t=`info`){let n=document.getElementById(`toastContainer`);if(!n)return;let r=document.createElement(`div`);r.className=`toast toast-${t}`,r.setAttribute(`role`,`status`),r.setAttribute(`aria-live`,`polite`),r.textContent=e,n.appendChild(r),requestAnimationFrame(()=>r.classList.add(`toast-visible`)),setTimeout(()=>{r.classList.remove(`toast-visible`),r.addEventListener(`transitionend`,()=>r.remove(),{once:!0}),setTimeout(()=>r.remove(),400)},3600)}var f={partition:`#F0E442`,intent:`#CC79A7`,runtime:`#0072B2`,config:`#009E73`,storage:`#56B4E9`,traffic:`#D55E00`,muted:`#8B949E`},p={healthy:`#00C369`,attention:`#FCB519`,failing:`#EE5F54`,pending:`#00ADE4`,drifted:`#FCB519`,"drifted-locked":`#F5A623`,neutral:`#566778`};function m(e){return e.kind===`partition`?200:e.kind===`intent`?230:220}function h(e){return e.kind===`partition`?76:72}function ee(e){return e.kind===`partition`?f.partition:e.kind===`intent`?f.intent:{Compute:f.runtime,Volume:f.storage,Config:f.config,ObjectStore:f.storage,Database:f.traffic,SQLDatabase:f.traffic,LoadBalancer:f.traffic,Observability:f.config}[e.assetType??``]??f.muted}function te(e){return e.kind===`partition`?`◫`:e.kind===`intent`?`⊞`:{Compute:`⧖`,Volume:`⊠`,Config:`≡`,ObjectStore:`⬜`,Database:`⫿`,SQLDatabase:`⫿`,LoadBalancer:`⊷`,Observability:`◎`}[e.assetType??``]??`⬡`}function ne(e){return e.kind===`partition`?`${e.meta?.reconciliation??`manual`} reconcile · ${e.meta?.deletionPolicy??`orphan`} delete`:e.kind===`intent`?e.meta?.target??e.displayStatus??`Intent`:`${e.assetType??`Asset`} · ${e.displayStatus??`Asset`}`}function re(e){return p[e.health??e.status??`neutral`]??p.neutral}function ie(e,t){let n=e.find(e=>e.kind===`partition`),r=e.filter(e=>e.kind===`intent`).sort((e,t)=>Number(e.level)-Number(t.level)||e.label.localeCompare(t.label)),i=e.filter(e=>e.kind===`asset`).sort((e,t)=>(e.parentID??``).localeCompare(t.parentID??``)||Number(e.level)-Number(t.level)||e.label.localeCompare(t.label)),a={},o=new Map;r.forEach(e=>{let t=Number(e.level??1);o.has(t)||o.set(t,[]),o.get(t).push(e)});let s=70;if([...o.keys()].sort((e,t)=>e-t).forEach(e=>{o.get(e).forEach(t=>{a[t.id]={x:260+(e-1)*320,y:s},s+=154}),s+=24}),n){let e=r.map(e=>a[e.id]).filter(Boolean),t=e.length?Math.min(...e.map(e=>e.y)):90,i=e.length?Math.max(...e.map(e=>e.y)):90;a[n.id]={x:40,y:Math.round((t+i)/2)}}r.forEach(e=>{let t=a[e.id];if(!t)return;let n=new Map;i.filter(t=>t.parentID===e.id).forEach(t=>{let r=Math.max(0,Number(t.level??0)-Number(e.level??0)-2);n.has(r)||n.set(r,[]),n.get(r).push(t)}),[...n.keys()].sort((e,t)=>e-t).forEach(e=>{let r=n.get(e),i=Math.max(0,(r.length-1)*96);r.forEach((n,r)=>{a[n.id]={x:t.x+280+e*250,y:t.y-i/2+r*96+e*8}})})});let c=Math.min(...Object.values(a).map(e=>e.y),40);if(c<40){let e=40-c;Object.values(a).forEach(t=>{t.y+=e})}return Object.entries(t).forEach(([e,t])=>{a[e]&&(a[e]={x:t.x,y:t.y})}),a}function g(e,t,n,r){let i=e.x+m(n),a=e.y+h(n)/2,o=t.x,s=t.y+h(r)/2,c=o-i,l=c>=0?1:-1,u=Math.max(70,Math.abs(c)/2);return`M ${i} ${a} C ${i+u*l} ${a}, ${o-u*l} ${s}, ${o} ${s}`}function ae(e,t,n,r){e.querySelectorAll(`path.topology-edge`).forEach((e,i)=>{let a=n[i];if(!a)return;let o=t[a.from],s=t[a.to],c=r.get(a.from),l=r.get(a.to);o&&s&&c&&l&&e.setAttribute(`d`,g(o,s,c,l))})}function _(e){let{canvas:t,topology:n,zoom:r,savedPositions:o,selectedNodeId:c,filters:l,onSelectNode:u,onDragNode:d}=e;if(!n?.nodes?.length){t.innerHTML=`<p class="empty-state" style="padding:24px">Select a partition to visualize its topology.</p>`;return}let f=n.nodes,p=new Map(f.map(e=>[e.id,e])),_=(n.edges??[]).filter(e=>l[e.kind]!==!1),v=ie(f,o),y=p.get(c)??f.find(e=>e.kind===`intent`)??f[0],oe=Math.max(...Object.values(v).map(e=>e.x+260),400),b=Math.max(...Object.values(v).map(e=>e.y+100),260),x=oe+40,S=b+40,C=[`<div class="topology-svg-frame">`,`<svg class="topology-svg" viewBox="0 0 ${x} ${S}" width="${Math.round(x*r)}" height="${Math.round(S*r)}" xmlns="http://www.w3.org/2000/svg">`,`<defs><filter id="ts"><feDropShadow dx="0" dy="6" stdDeviation="10" flood-color="rgba(0,0,0,0.28)"/></filter></defs>`];_.forEach(e=>{let t=v[e.from],n=v[e.to],r=p.get(e.from),o=p.get(e.to);if(!(!t||!n||!r||!o)&&(C.push(`<path class="topology-edge ${a(e.kind)}" d="${g(t,n,r,o)}" />`),e.label)){let a=t.x+m(r),s=t.y+h(r)/2,c=n.x,l=n.y+h(o)/2;C.push(`<text class="topology-edge-label" x="${(a+c)/2}" y="${(s+l)/2-10}">${i(e.label)}</text>`)}}),f.forEach(e=>{let t=v[e.id];if(!t)return;let n=ee(e),r=y?.id===e.id,o=m(e),c=h(e);C.push(`
      <g class="topology-node ${a(e.kind)}${r?` selected`:``}" data-node="${a(e.id)}" transform="translate(${t.x},${t.y})">
        <rect class="topology-node-card" width="${o}" height="${c}" rx="12" filter="url(#ts)" />
        <rect class="topology-node-accent" width="4" height="${c}" rx="4" fill="${n}" />
        <circle cx="18" cy="18" r="5.5" fill="${re(e)}" />
        <text x="32" y="20" class="topology-node-title">${i(`${te(e)} ${e.label}`)}</text>
        <text x="32" y="38" class="topology-node-subtitle">${i(ne(e))}</text>
        <text x="14" y="60" class="topology-node-description">${i(s(e.description??``,68))}</text>
      </g>
    `)}),C.push(`</svg>`,`</div>`),t.innerHTML=C.join(``);let w=t.querySelector(`svg.topology-svg`);w.querySelectorAll(`[data-node]`).forEach(e=>{let t=e.dataset.node;function n(e,t){let n=w.createSVGPoint();n.x=e,n.y=t;let r=w.getScreenCTM();if(!r)return{x:e,y:t};let i=n.matrixTransform(r.inverse());return{x:i.x,y:i.y}}let r=!1,i=!1,a=0,o=0,s=0,c=0;e.addEventListener(`pointerdown`,n=>{if(n.button!==0)return;r=!0,i=!1,a=n.clientX,o=n.clientY;let l=v[t];s=l?l.x:0,c=l?l.y:0,e.setPointerCapture(n.pointerId),n.stopPropagation()}),e.addEventListener(`pointermove`,l=>{if(!r)return;let u=n(a,o),f=n(l.clientX,l.clientY),m=s+(f.x-u.x),h=c+(f.y-u.y);!i&&Math.abs(m-s)<3&&Math.abs(h-c)<3||(i=!0,v[t]={x:m,y:h},e.setAttribute(`transform`,`translate(${m},${h})`),e.classList.add(`dragging`),ae(w,v,_,p),d(t,{...v}),l.stopPropagation())}),e.addEventListener(`pointerup`,n=>{r&&(r=!1,e.releasePointerCapture(n.pointerId),e.classList.remove(`dragging`),i&&d(t,{...v}),n.stopPropagation())}),e.addEventListener(`click`,e=>{if(i){e.stopPropagation(),e.preventDefault();return}u(t,{...v})})})}function v(e){e&&(e.innerHTML=`
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
  `)}var y=e(),oe=2e4,b=4e3,x=6e4,S=6e4,C=6e4;document.addEventListener(`DOMContentLoaded`,()=>{pe(),mt(),w().catch($)});async function w(){fe(y.activePanel),await E(),se(),y.activePanel===`rolloutsPanel`&&y.selectedPartition&&T()}function se(){y.refreshTimer!==void 0&&window.clearTimeout(y.refreshTimer),y.refreshTimer=window.setTimeout(async()=>{try{await E(y.activePanel!==`historyPanel`&&y.activePanel!==`rolloutsPanel`)}catch{}finally{se()}},ce())}function ce(){return document.hidden?x:Date.now()<y.fastRefreshUntil||le()?b:oe}function le(){let e=y.detail;if(!e)return!1;if(String(e?.health?.status??``).toLowerCase()===`pending`)return!0;let t=Array.isArray(e?.intents)?e.intents:[];for(let e of t)switch(String(e?.status??``)){case`Checking`:case`Diffing`:case`Applying`:case`Destroying`:case`Ready`:case`Blocked`:return!0;default:break}return(Array.isArray(e?.health?.services)?e.health.services:[]).some(e=>e?.taskActive===!0)}function ue(e=S){let t=Date.now()+e;t>y.fastRefreshUntil&&(y.fastRefreshUntil=t)}function T(){y.rolloutsRefreshTimer!==void 0&&window.clearTimeout(y.rolloutsRefreshTimer),y.rolloutsRefreshTimer=window.setTimeout(async()=>{try{await A(!0)}catch{}finally{T()}},C)}function de(){y.rolloutsRefreshTimer!==void 0&&(window.clearTimeout(y.rolloutsRefreshTimer),y.rolloutsRefreshTimer=void 0)}async function E(e=!0){r(`Refreshing…`),y.overview=await t(`/api/overview`),be(),j();let n=y.selectedPartition,i=(y.overview?.partitions??[]).map(e=>e.name);!n&&i.length>0?await D(i[0],!1):n&&i.includes(n)&&e?await D(n,!1):i.length||(y.selectedPartition=``,y.detail=null,y.history=null,y.rollouts=null,y.expandedRolloutKeys={},P(),z(),B(),V(),O()),r(`Updated just now`)}async function D(e,n=!0){if(!e)return;ue();let r=y.selectedPartition===e;y.selectedPartition=e,y.activityDrawer={intentName:``,data:null,loading:!1,error:``},r||(y.expandedAssetKey=``,y.expandedRolloutKeys={},y.diagnosticDetails={},y.history=null,y.historyLoading=!1,y.historyError=``,y.rollouts=null,y.rolloutsLoading=!1,y.rolloutsError=``),O(),j(),y.detail=await t(`/api/partitions/${encodeURIComponent(e)}`),P(),z(),B(),V(),H(),y.activePanel===`historyPanel`&&k().catch($),y.activePanel===`rolloutsPanel`&&(A(r).catch($),T())}function fe(e){y.activePanel=e,O(),document.querySelectorAll(`.panel`).forEach(t=>{let n=t.id===e;t.classList.toggle(`active`,n),t.classList.toggle(`hidden`,!n)}),document.querySelectorAll(`[data-tab-target]`).forEach(t=>{t.classList.toggle(`active`,t.dataset.tabTarget===e)}),j(),P(),z(),B(),H(),e===`historyPanel`&&y.selectedPartition&&k().catch($),e===`rolloutsPanel`&&y.selectedPartition&&(A(!0).catch($),T()),e!==`rolloutsPanel`&&de()}function pe(){let e=new URLSearchParams(window.location.search),t=e.get(`partition`);t&&(y.selectedPartition=t.trim());let n=e.get(`panel`);[`overviewPanel`,`topologyPanel`,`rolloutsPanel`,`historyPanel`].includes(n??``)&&(y.activePanel=n);let r=Number.parseInt(e.get(`historyLimit`)??``,10);Number.isFinite(r)&&r>0&&(y.historyOptions.limit=r);let i=e.get(`historySince`);i&&(y.historyOptions.since=i);let a=e.get(`historyUntil`);a&&(y.historyOptions.until=a),ge()}function O(){let e=new URLSearchParams(window.location.search);y.selectedPartition?e.set(`partition`,y.selectedPartition):e.delete(`partition`),y.activePanel&&y.activePanel!==`overviewPanel`?e.set(`panel`,y.activePanel):e.delete(`panel`),y.historyOptions.limit===10?e.delete(`historyLimit`):e.set(`historyLimit`,String(y.historyOptions.limit)),y.historyOptions.since?e.set(`historySince`,y.historyOptions.since):e.delete(`historySince`),y.historyOptions.until?e.set(`historyUntil`,y.historyOptions.until):e.delete(`historyUntil`);let t=e.toString();window.history.replaceState(null,``,`${window.location.pathname}${t?`?${t}`:``}`)}async function me(e){let n=new URLSearchParams;return n.set(`limit`,String(y.historyOptions.limit)),y.historyOptions.since&&n.set(`since`,y.historyOptions.since),y.historyOptions.until&&n.set(`until`,y.historyOptions.until),t(`/api/partitions/${encodeURIComponent(e)}/history?${n.toString()}`)}async function he(e){return t(`/api/partitions/${encodeURIComponent(e)}/rollouts`)}function ge(){let e=document.getElementById(`historyLimit`);e&&(e.value=String(y.historyOptions.limit));let t=document.getElementById(`historySince`);t&&(t.value=l(y.historyOptions.since));let n=document.getElementById(`historyUntil`);n&&(n.value=l(y.historyOptions.until))}function _e(){let e=document.getElementById(`historyLimit`),t=Number.parseInt(e?.value??``,10);y.historyOptions.limit=Number.isFinite(t)&&t>0?t:10;let n=document.getElementById(`historySince`);y.historyOptions.since=u(n?.value??``);let r=document.getElementById(`historyUntil`);y.historyOptions.until=u(r?.value??``)}async function ve(){if(_e(),O(),!y.selectedPartition){z();return}await k(!0)}async function ye(){y.historyOptions={limit:10,since:``,until:``},ge(),await ve()}async function k(e=!1){if(!y.selectedPartition){y.history=null,y.historyLoading=!1,y.historyError=``,z(),H();return}if(!y.historyLoading){if(!e&&y.history){z(),H();return}y.historyLoading=!0,y.historyError=``,z(),H();try{y.history=await me(y.selectedPartition)}catch(e){throw y.history=null,y.historyError=e?.message??`Failed to load history.`,e}finally{y.historyLoading=!1,z(),H()}}}async function A(e=!1){if(!y.selectedPartition){y.rollouts=null,y.rolloutsLoading=!1,y.rolloutsError=``,B(),H();return}if(!y.rolloutsLoading){if(!e&&y.rollouts){B(),H();return}y.rolloutsLoading=!0,y.rolloutsError=``,B(),H();try{y.rollouts=await he(y.selectedPartition)}catch(e){throw y.rollouts=null,y.rolloutsError=e?.message??`Failed to load rollouts.`,e}finally{y.rolloutsLoading=!1,B(),H()}}}function be(){let e=y.overview?.summary??{};n(`summaryPartitions`,e.partitions??0),n(`summaryIntents`,e.intents??0),n(`summaryAssets`,e.assets??0),n(`summaryStable`,e.healthyAssets??e.servicesHealthy??0),n(`summaryAttention`,e.attentionAssets??e.servicesAttention??0),n(`summaryFailed`,e.failingAssets??e.failedIntents??0)}function j(){let e=y.activePanel===`overviewPanel`&&!y.selectedPartition;M(),e?N():xe()}function xe(){let e=document.getElementById(`appGrid`);e&&(e.className=`grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5`,e.innerHTML=``)}function M(){let e=document.getElementById(`partitionList`);if(!e)return;if(!y.overview){e.className=`grid gap-1 loading-state text-sm text-[#566778]`,e.textContent=`Loading partitions…`;return}let t=document.getElementById(`partitionSearch`)?.value.trim().toLowerCase()??``,n=(y.overview?.partitions??[]).filter(e=>t?`${e.name} ${Object.keys(e.labels??{}).join(` `)} ${Object.values(e.labels??{}).join(` `)}`.toLowerCase().includes(t):!0);if(!n.length){e.className=`grid gap-1 empty-state text-sm text-[#566778]`,e.textContent=`No partitions available.`;return}e.className=`grid gap-1`,e.innerHTML=n.map(e=>{let t=e.name===y.selectedPartition,n=Ge([e.errors?.join(`
`),e.lastDisplayStatus?`Last known status: ${e.lastDisplayStatus}`:``]);return`
      <button class="partition-list-item ${t?`active`:``}" data-partition="${a(e.name)}">
        <div class="partition-list-title">
          <strong>${i(e.name)}</strong>
          ${U(e.health,e.displayStatus,`${e.name} status`,n,`partition:${e.name}`)}
        </div>
        <div class="partition-list-meta">
          <span>${e.intentCount??0} intents</span>
          <span>${e.assetCount??0} assets</span>
          <span>${e.healthyAssets??e.servicesHealthy??0} stable</span>
        </div>
      </button>
    `}).join(``),e.querySelectorAll(`[data-partition]`).forEach(e=>{e.addEventListener(`click`,()=>D(e.dataset.partition).catch($))})}function N(){let e=document.getElementById(`appGrid`);if(!e)return;let t=(document.getElementById(`appGridSearch`)?.value??``).trim().toLowerCase(),n=(y.overview?.partitions??[]).filter(e=>t?`${e.name} ${Object.values(e.labels??{}).join(` `)}`.toLowerCase().includes(t):!0);if(!n.length){e.className=`grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5 empty-state text-sm text-[#566778]`,e.textContent=y.overview?`No partitions match the filter.`:`Loading partitions…`;return}e.className=`grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5`,e.innerHTML=n.map(e=>{let t=e.name===y.selectedPartition,n=Xe(e.labels??{});return`
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
    `}).join(``),e.querySelectorAll(`[data-partition]`).forEach(e=>{e.addEventListener(`click`,()=>D(e.dataset.partition).catch($))})}function P(){let e=y.detail,t=document.getElementById(`heroContent`);if(!t)return;if(y.activePanel!==`overviewPanel`){H();return}if(!e){t.className=`loading-state text-sm text-[#566778]`,t.textContent=`Select a partition to inspect its current shape.`,[`intentCards`,`attentionAssetsList`,`serviceHealthCards`,`recentEventsList`].forEach(e=>{let t=document.getElementById(e);t&&(t.className=`loading-state text-sm text-[#566778]`,t.textContent=`Choose a partition.`)}),H();return}let n=e.health??{},r={...e.partition?.manifest?.metadata?.labels??{},...e.partition?.manifest?.spec?.labels??{}};t.className=``,t.innerHTML=`
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
        ${n.status===`pending`?Ye(e):``}
        <div class="pill-row mt-2">
          ${r.endpoint?`<span class="pill">${i(r.endpoint)}</span>`:``}
          ${r.topology?`<span class="pill">${i(r.topology)}</span>`:``}
          ${r.managedBy?`<span class="pill">${i(r.managedBy)}</span>`:``}
          ${(e.partition.state?.errors??[]).map(e=>`<span class="pill">${i(e)}</span>`).join(``)}
          ${e.compilerError?`<span class="pill">${i(e.compilerError)}</span>`:``}
        </div>
      </div>
      ${F(`Healthy`,n.healthy??0)}
      ${F(`Attention`,(n.attention??0)+(n.pending??0))}
      ${F(`Failing`,n.failing??0)}
    </div>
  `,Se(),L(),je(),Pe(),H()}function F(e,t){return`
    <div class="stat-card rounded-lg border border-white/[0.09]">
      <div class="stat-label">${i(e)}</div>
      <div class="stat-value">${t}</div>
    </div>
  `}function Se(){let e=document.getElementById(`attentionAssetsList`);if(!e)return;let t=Ce(),n=we();if(!t.length&&!n.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No assets need attention right now.`;return}e.className=`attention-asset-list`,e.innerHTML=t.map(({intent:e,asset:t})=>{let n=I(t);return`
    <article class="attention-asset-card attention-asset-card-${a(t.health)}">
      <div class="attention-asset-card-header">
        <div>
          <h3>${i(t.name)}</h3>
          <div class="muted">${i(e.name)} · ${i(Q(t.type))}</div>
        </div>
        ${U(t.health,t.displayStatus,`${e.name} / ${t.name}`,t.summary,`asset:${y.selectedPartition}:${e.name}:${t.name}`)}
      </div>
      <p class="muted mt-1">${i(n)}</p>
      <div class="pill-row mt-2">
        <span class="pill">${i(e.targetSummary??`Unassigned`)}</span>
        ${(t.quickFacts??[]).slice(0,3).map(e=>`<span class="${e.label===`Release`?`pill pill-release`:`pill`}">${i(`${e.label}: ${e.value}`)}</span>`).join(``)}
      </div>
    </article>
  `}).join(``)+(n.length?`
    <div class="progressing-asset-list mt-2">
      <div class="progressing-assets-header">Progressing — awaiting first reconcile (${n.length})</div>
      ${n.map(({intent:e,asset:t})=>{let n=I(t);return`
        <div class="progressing-asset-item">
          <div>
            <div>${i(t.name)}</div>
            <div class="muted">${i(e.name)} · ${i(Q(t.type))}${n?` · ${i(n)}`:``}</div>
          </div>
          ${U(t.health,t.displayStatus,`${e.name} / ${t.name}`,t.summary,`asset:${y.selectedPartition}:${e.name}:${t.name}`)}
        </div>
      `}).join(``)}
    </div>
  `:``)}function Ce(){return(y.detail?.intents??[]).flatMap(e=>(e.assets??[]).map(t=>({intent:e,asset:t}))).filter(({asset:e})=>e?.health===`failing`||e?.health===`attention`).sort((e,t)=>{let n=Te(e.asset.health)-Te(t.asset.health);return n===0?e.intent.name===t.intent.name?e.asset.name.localeCompare(t.asset.name):e.intent.name.localeCompare(t.intent.name):n})}function we(){return(y.detail?.intents??[]).flatMap(e=>(e.assets??[]).map(t=>({intent:e,asset:t}))).filter(({asset:e})=>e?.health===`pending`).sort((e,t)=>e.intent.name===t.intent.name?e.asset.name.localeCompare(t.asset.name):e.intent.name.localeCompare(t.intent.name))}function I(e){let t=String(e?.summary??``).trim(),n=String(e?.observedHealth?.summary??``).trim(),r=String(e?.status??``);return(r===`Drifted`||r===`DriftedLocked`)&&n?t.includes(n)?t:t?`${t}: ${n}`:n:t}function Te(e){return e===`failing`?0:e===`attention`?1:2}function L(){let e=document.getElementById(`intentCards`);if(!e)return;let t=y.detail?.intents??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No intents defined for this partition yet.`;return}e.className=`intent-stack`,e.innerHTML=t.map(e=>{let t=ft(e.assets??[]),n=y.activityDrawer.intentName===e.name,r=pt(e.assets??[]).map(t=>`
      <section class="intent-asset-group">
        <div class="intent-asset-group-title">
          <span class="intent-lane-group-dot" style="background:${dt(t.category)}"></span>
          <span>${i(t.category)} · ${t.assets.length}</span>
        </div>
        <div class="asset-grid">
          ${t.assets.map(t=>{let n=I(t),r=Ee(e.name,t.name),o=y.expandedAssetKey===r,s=Z(t.type),c=`asset-detail-${R(r)}`,l=[...t.quickFacts??[]].sort((e,t)=>e.label===`Release`?-1:+(t.label===`Release`)).map(e=>`<span class="fact${at(e.label)}" title="${a(ot(e.label))}">${i(e.label)}: ${i(e.value)}</span>`).join(``),u=o?X(t,{limit:2**53-1,truncateAt:160}):``,d=o?ct(t.outputs??{},[],{limit:2**53-1,truncateAt:160}):``,f=o?(t.references??[]).map(e=>`<span class="fact">${i(e)}</span>`).join(``):``,p=o&&(t.dependsOn??[]).length?(t.dependsOn??[]).map(e=>`<span class="fact">${i(e)}</span>`).join(``):``;return`
              <article
                class="asset-chip asset-chip-${a(t.health??`neutral`)}${o?` asset-chip-expanded`:``}"
                data-asset-toggle="${a(r)}"
                data-asset-card="${a(R(r))}"
                role="button"
                tabindex="0"
                aria-expanded="${o?`true`:`false`}"
                aria-controls="${a(c)}"
              >
                <div class="asset-chip-top">
                  <div>
                    <div class="asset-chip-title">${i(t.name)}</div>
                    <div class="asset-chip-type-row">
                      <span class="asset-chip-type">${i(Q(t.type))}</span>
                      <span class="asset-chip-category">${i(s)}</span>
                    </div>
                  </div>
                  ${U(t.health,t.displayStatus,`${e.name} / ${t.name}`,t.summary,`asset:${y.selectedPartition}:${e.name}:${t.name}`)}
                </div>
                ${n?`<div class="muted mt-1">${i(n)}</div>`:``}
                ${l?`<div class="fact-row">${l}</div>`:``}
                <div class="asset-chip-toggle-row">
                  <span class="asset-chip-toggle-copy">${o?`Hide full asset details`:`Show image, mounts, outputs, and manifest details`}</span>
                  <span class="asset-chip-toggle-indicator" aria-hidden="true">${o?`−`:`+`}</span>
                </div>
                ${o?`
                  <div class="asset-chip-details" id="${a(c)}">
                    ${p?`<div class="asset-chip-detail-block"><div class="asset-chip-detail-heading">Depends on</div><div class="fact-row">${p}</div></div>`:``}
                    ${u?`<div class="asset-chip-detail-block"><div class="asset-chip-detail-heading">Manifest details</div><div class="fact-row">${u}</div></div>`:``}
                    ${d?`<div class="asset-chip-detail-block"><div class="asset-chip-detail-heading">Outputs</div><div class="fact-row">${d}</div></div>`:``}
                    ${f?`<div class="asset-chip-detail-block"><div class="asset-chip-detail-heading">Output refs</div><div class="fact-row">${f}</div></div>`:``}
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
              ${U(e.health,e.displayStatus,`${e.name} intent`,e.summary,`intent:${y.selectedPartition}:${e.name}`)}
              <span class="pill">${i(e.targetSummary??`Unassigned`)}</span>
              ${(e.joined??[]).map(e=>`<span class="pill">joins ${i(e)}</span>`).join(``)}
              ${t.map(e=>`<span class="pill">${i(`${e.category} ${e.count}`)}</span>`).join(``)}
              ${e.locked?`<span class="pill">locked</span>`:``}
              <button class="activity-btn ${n?`active`:``}" type="button" data-activity-intent="${a(e.name)}">&#9685;</button>
            </div>
          </div>
        </div>
        ${n?Ae():``}
        ${r}
      </article>
    `}).join(``),e.querySelectorAll(`[data-activity-intent]`).forEach(e=>{e.addEventListener(`click`,()=>ke(e.dataset.activityIntent).catch($))}),e.querySelectorAll(`[data-asset-toggle]`).forEach(e=>{e.addEventListener(`click`,()=>De(e.dataset.assetToggle??``)),e.addEventListener(`keydown`,t=>{t.key!==`Enter`&&t.key!==` `||(t.preventDefault(),De(e.dataset.assetToggle??``))})})}function Ee(e,t){return`${e}::${t}`}function R(e){return e.replace(/[^a-zA-Z0-9_-]+/g,`-`)}function De(e){e&&(y.expandedAssetKey=y.expandedAssetKey===e?``:e,L())}function Oe(e){if(!e)return;y.expandedAssetKey=e,L();let t=R(e);requestAnimationFrame(()=>{document.querySelector(`[data-asset-card="${t}"]`)?.scrollIntoView({behavior:`smooth`,block:`center`,inline:`nearest`})})}async function ke(e){if(y.activityDrawer.intentName===e){y.activityDrawer={intentName:``,data:null,loading:!1,error:``},L();return}y.activityDrawer={intentName:e,data:null,loading:!0,error:``},L();try{let n=y.selectedPartition;y.activityDrawer={intentName:e,data:await t(`/api/partitions/${encodeURIComponent(n)}/intents/${encodeURIComponent(e)}/activity`),loading:!1,error:``}}catch(t){y.activityDrawer={intentName:e,data:null,loading:!1,error:t.message??`Failed to load activity`}}L()}function Ae(){let{data:e,loading:t,error:n}=y.activityDrawer;if(t)return`<div class="activity-drawer"><div class="activity-loading">Loading activity…</div></div>`;if(n)return`<div class="activity-drawer"><div class="activity-error">${i(n)}</div></div>`;if(!e)return`<div class="activity-drawer"><div class="activity-loading">No activity data.</div></div>`;let r=e.timestamps??{},o=[{label:`Queued`,value:r.lastQueuedAt},{label:`Check`,value:r.lastCheckAt},{label:`Diff`,value:r.lastDiffAt},{label:`Apply`,value:r.lastApplyAt}].filter(e=>e.value&&e.value!==`0001-01-01T00:00:00Z`),s=e.logs??[],l=e.drift;return`
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
  `}function je(){let e=document.getElementById(`serviceHealthCards`);if(!e)return;let t=y.detail?.health?.services??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No service-like assets to score yet.`;return}let n=t.filter(e=>e.status===`healthy`).length,r=t.filter(e=>e.status===`attention`).length,o=t.filter(e=>e.status===`failing`).length,s=t.filter(e=>e.taskActive).length,c=t.filter(e=>e.taskTimedOut).length;e.className=`service-stack`,e.innerHTML=`
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
          <div class="muted">${i(e.intent)} · ${i(Q(e.type))}</div>
        </div>
        ${U(e.status,e.displayStatus,`${e.intent} / ${e.asset}`,e.summary,`service:${y.selectedPartition}:${e.intent}:${e.asset}`)}
      </div>
      <p class="service-card-note">${i(Me(e))}</p>
      <div class="service-health-meta">
        ${Ne(e)}
      </div>
      <div class="service-card-actions">
        <button class="btn-secondary service-card-action" type="button" data-service-focus="${a(Ee(e.intent,e.asset))}">Open details</button>
      </div>
    </article>
  `).join(``)}
  `,e.querySelectorAll(`[data-service-focus]`).forEach(e=>{e.addEventListener(`click`,()=>Oe(e.dataset.serviceFocus??``))})}function Me(e){if(e.taskTimedOut)return`Last reconcile task timed out. Open details in the intent map for config and outputs.`;if(e.taskActive)return`Reconcile is currently running for this service.`;switch(e.status){case`healthy`:return`Operational summary only. Configuration and ports stay in the intent map.`;case`pending`:return`Waiting for the first successful reconcile.`;case`attention`:return String(e.summary??`Needs attention.`);case`failing`:return String(e.summary??`Service is failing.`);default:return String(e.summary??`Service status unavailable.`)}}function Ne(e){let t=[];e.taskActive&&t.push(`reconcile running`),e.taskTimedOut&&t.push(`last task timed out`);let n=c(e.lastUpdatedAt);return n!==`—`&&t.push(`updated ${n}`),t.map(e=>`<span class="service-health-meta-item">${i(e)}</span>`).join(``)}function Pe(){let e=document.getElementById(`recentEventsList`);if(!e)return;let t=y.detail?.recentEvents??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Recent event history loads only in the History tab.`;return}e.className=`timeline-stack`,e.innerHTML=ze(t).map(Be).join(``)}function z(){let e=document.getElementById(`deploymentTimeline`),t=document.getElementById(`eventTimeline`);if(!e||!t)return;if(!y.selectedPartition){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a partition to inspect deployment history.`,t.className=`empty-state text-sm text-[#566778]`,t.textContent=`Select a partition to inspect event history.`;return}if(y.historyLoading){e.className=`loading-state text-sm text-[#566778]`,e.textContent=`Loading deployment history…`,t.className=`loading-state text-sm text-[#566778]`,t.textContent=`Loading event history…`;return}if(y.historyError){e.className=`empty-state text-sm text-[#566778]`,e.textContent=y.historyError,t.className=`empty-state text-sm text-[#566778]`,t.textContent=y.historyError;return}let n=y.history;if(!n){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Open the History tab to load deployment history.`,t.className=`empty-state text-sm text-[#566778]`,t.textContent=`Open the History tab to load event history.`;return}let r=(document.getElementById(`historyFilter`)?.value??``).trim().toLowerCase(),i=(n.deployments??[]).filter(e=>r?`${e.intent} ${e.deploymentRevision} ${(e.assets??[]).map(e=>`${e.asset} ${e.version??``}`).join(` `)}`.toLowerCase().includes(r):!0),a=(n.events??[]).filter(e=>r?`${e.intent??``} ${e.title??``} ${e.message??``}`.toLowerCase().includes(r):!0);e.className=i.length?`timeline-stack`:`empty-state text-sm text-[#566778]`,e.innerHTML=i.length?i.map(Ve).join(``):`No deployment entries match the current filter.`;let o=document.getElementById(`historyGroupToggle`),s=!o||o.checked?ze(a):a;t.className=s.length?`timeline-stack`:`empty-state text-sm text-[#566778]`,t.innerHTML=s.length?s.map(Be).join(``):`No events match the current filter.`}function B(){let e=document.getElementById(`rolloutsTimeline`);if(!e)return;if(!y.selectedPartition){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a partition to inspect rollout history.`;return}if(y.rolloutsLoading){e.className=`loading-state text-sm text-[#566778]`,e.textContent=`Loading rollouts…`;return}if(y.rolloutsError){e.className=`empty-state text-sm text-[#566778]`,e.textContent=y.rolloutsError;return}let t=y.rollouts?.rollouts??[];e.className=t.length?`timeline-stack`:`empty-state text-sm text-[#566778]`,e.innerHTML=t.length?t.map(Fe).join(``):`No archived rollouts were found for this partition yet.`,e.querySelectorAll(`[data-rollout-toggle]`).forEach(e=>{e.addEventListener(`click`,t=>{t.preventDefault();let n=e.dataset.rolloutToggle??``;n&&Ie(n)})})}function Fe(e){let t=e.assets??[],n=t.length,r=Le(e),s=!!y.expandedRolloutKeys[r],l=e.current?U(`healthy`,`Current`):e.newIntent?U(`pending`,`New intent`):U(`healthy`,`Rollout`);return`
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
                ${U(Re(t.change),o(t.change||`updated`))}
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
  `}function Ie(e){e&&(y.expandedRolloutKeys={...y.expandedRolloutKeys,[e]:!y.expandedRolloutKeys[e]},B())}function Le(e){return`${e.intent??``}::${e.deploymentRevision??``}`}function Re(e){switch((e??``).toLowerCase()){case`added`:return`pending`;case`removed`:return`attention`;default:return`healthy`}}function ze(e){let t=new Map;for(let n of e){let e=`${n.intent??``}::${n.title}`,r=t.get(e);!r||new Date(n.timestamp)>new Date(r.latest.timestamp)?t.set(e,{latest:n,count:(r?.count??0)+1}):r.count++}return Array.from(t.values()).sort((e,t)=>new Date(t.latest.timestamp).getTime()-new Date(e.latest.timestamp).getTime()).map(e=>({...e.latest,groupCount:e.count}))}function Be(e){let t=e.groupCount>1?`<span class="event-count-pill" title="${e.groupCount} occurrences">${e.groupCount}×</span>`:``,n=(e.title??``).toLowerCase().replace(/[^a-z0-9]/g,``),r=(e.message??``).toLowerCase().replace(/[^a-z0-9]/g,``),a=e.message&&r!==n;return`
    <article class="timeline-card">
      <div class="timeline-head">
        <div>
          <span class="event-type-eyebrow">Event type</span>
          <h3>${i(e.title??`Event`)} ${t}</h3>
          ${a?`<div class="muted">${i(e.message)}</div>`:``}
        </div>
        ${U(e.status,e.displayStatus,e.title??`Event`,e.message??``)}
      </div>
      <div class="timeline-meta">
        <span>${c(e.timestamp)}</span>
        ${e.intent?`<span>${i(e.intent)}</span>`:``}
        ${e.taskID?`<span>${i(e.taskID)}</span>`:``}
        ${e.deploymentRevision?`<span>${i(e.deploymentRevision)}</span>`:``}
      </div>
    </article>
  `}function Ve(e){return`
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
  `}function V(){let e=document.getElementById(`topologyCanvas`);if(!e)return;let t=y.detail?.topology;v(document.getElementById(`topologyLegend`)),_({canvas:e,topology:t,zoom:y.topology.zoom,savedPositions:y.topology.nodePositions,selectedNodeId:y.topology.selectedNodeId,filters:{contains:document.getElementById(`showContainEdges`)?.checked??!0,join:document.getElementById(`showJoinEdges`)?.checked??!0,dependsOn:document.getElementById(`showAssetEdges`)?.checked??!0,outputRef:document.getElementById(`showOutputEdges`)?.checked??!0},onSelectNode:(e,t)=>{y.topology.selectedNodeId=e,y.topology.nodePositions=t,He(),V()},onDragNode:(e,t)=>{y.topology.nodePositions=t}}),He()}function He(){let e=document.getElementById(`topologyDetails`);if(!e)return;let t=y.detail?.topology;if(!t?.nodes?.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a node to inspect its status, metadata, and linked details.`;return}let n=new Map(t.nodes.map(e=>[e.id,e])),r=n.get(y.topology.selectedNodeId);if(!r){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a node to inspect its status, metadata, and linked details.`;return}let a={contains:document.getElementById(`showContainEdges`)?.checked??!0,join:document.getElementById(`showJoinEdges`)?.checked??!0,dependsOn:document.getElementById(`showAssetEdges`)?.checked??!0,outputRef:document.getElementById(`showOutputEdges`)?.checked??!0},s=(t.edges??[]).filter(e=>a[e.kind]!==!1).filter(e=>e.from===r.id||e.to===r.id),c=r.kind===`asset`?Qe(r.intent,r.asset??r.label):null,l=r.kind===`intent`?Ze(r.intent??r.label):null,u=c?X(c):``,d=ct(c?.outputs??l?.outputs??{},c?[]:l?.outputHints??[]),f=(c?.references??[]).map(e=>`<span class="fact">${i(e)}</span>`).join(``),p=s.map(e=>{let t=e.from===r.id?e.to:e.from,a=n.get(t);return a?`
      <div class="topology-detail-row">
        <span class="topology-detail-direction">${e.from===r.id?`out`:`in`}</span>
        <span class="topology-detail-name">${i(a.label)}</span>
        <span class="topology-detail-kind">${i(o(e.kind))}</span>
      </div>
    `:``}).join(``);e.className=`topology-detail-card`,e.innerHTML=`
    <div style="--node-accent:${$e(r)}">
      <div class="topology-detail-header">
        <div class="topology-detail-icon">${i(et(r))}</div>
        <div>
          <h3>${i(r.label)}</h3>
          <p>${i(tt(r))}</p>
        </div>
      </div>
      <div class="pill-row mb-2">
        ${U(r.health??r.status,r.displayStatus??o(r.kind),r.label,r.description,`topology:${y.selectedPartition}:${r.id}`)}
        <span class="pill">${i(o(r.kind))}</span>
        ${r.assetType?`<span class="pill">${i(Q(r.assetType))}</span>`:``}
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
  `}var Ue={overviewPanel:{eyebrow:`Control center`,title:`Control Center`},topologyPanel:{eyebrow:`Deployment graph`,title:`Topology`},rolloutsPanel:{eyebrow:`Release timeline`,title:`Rollouts`},historyPanel:{eyebrow:`Push timeline`,title:`History`}};function H(){let e=y.activePanel,t=y.detail,r=Ue[e]??Ue.overviewPanel,a=e===`overviewPanel`,o=!!y.selectedPartition,s=a&&!o,c=(e,t)=>{let n=document.getElementById(e);n&&(n.style.display=t?``:`none`)};c(`appGridSection`,s),c(`summaryGrid`,s),c(`selectedPartitionHero`,a&&o),c(`sidebarPartitionSection`,!0),n(`pageEyebrow`,r.eyebrow),n(`pageTitle`,r.title);let l=`Monitor partitions, inspect topology, and review history.`,u=``;e===`overviewPanel`&&t&&(l=`${t.partition.manifest.spec?.deletionPolicy??`orphan`} policy · ${t.partition.manifest.spec?.reconciliation?.mode??`manual`} reconcile · ${t.intents.length} intents`,u=`${U(t.health?.status,t.health?.displayStatus??`Selected`,`${t.partition.manifest.metadata.name} health`,t.health?.summary,`partition-health:${t.partition.manifest.metadata.name}`)} <span class="pill">${t.topology?.nodes?.length??0} nodes</span>`),e===`topologyPanel`&&(l=t?`Topology for ${t.partition.manifest.metadata.name}.`:`Select a partition to inspect its graph.`,u=t?`<span class="pill">${i(t.partition.manifest.metadata.name)}</span><span class="pill">${t.topology?.nodes?.length??0} nodes</span>`:``),e===`rolloutsPanel`&&(l=t?`Review archived rollout changes for ${t.partition.manifest.metadata.name}.`:`Select a partition to inspect rollout history.`,u=t?`<span class="pill">${i(t.partition.manifest.metadata.name)}</span><span class="pill">${y.rollouts?.rollouts?.length??0} rollouts</span>`:``),e===`historyPanel`&&(l=t?`Review deployments and state changes for ${t.partition.manifest.metadata.name}.`:`Select a partition to inspect history.`,u=t?`<span class="pill">${i(t.partition.manifest.metadata.name)}</span><span class="pill">${y.history?.deployments?.length??0} deployments</span>`:``),n(`pageSubtitle`,l);let d=document.getElementById(`headerContextPills`);d&&(d.innerHTML=u,d.style.display=u.trim()?``:`none`);let f=document.getElementById(`topnavPartition`);f&&(f.textContent=y.selectedPartition||t?.partition?.manifest?.metadata?.name||r.title||`Control Center`)}function U(e,t,n,r,s){let c=String(e??`neutral`).toLowerCase(),l=t??o(c),u=We(s,c,r);return(c===`failing`||c===`attention`)&&u.length>0?`<button type="button" class="badge badge-${a(c)} badge-clickable" data-diagnostic-title="${a((n??l).trim())}" data-diagnostic-detail="${a(u)}" aria-label="Show diagnostic details for ${a(l)}">${i(l)}</button>`:`<span class="badge badge-${a(c)}">${i(l)}</span>`}function We(e,t,n){let r=String(e??``).trim(),i=String(n??``).trim();return r?t===`failing`||t===`attention`?i?(y.diagnosticDetails[r]=i,i):y.diagnosticDetails[r]??``:(delete y.diagnosticDetails[r],i):i}function Ge(e){return e.map(e=>String(e??``).trim()).filter(e=>e.length>0).join(`
`)}function Ke(){let e=document.getElementById(`diagnosticsModal`);if(e)return e;let t=document.createElement(`div`);return t.id=`diagnosticsModal`,t.className=`diagnostics-modal hidden`,t.innerHTML=`
    <div class="diagnostics-modal-card" role="dialog" aria-modal="true" aria-labelledby="diagnosticsModalTitle">
      <div class="diagnostics-modal-header">
        <h3 id="diagnosticsModalTitle">Status details</h3>
        <button type="button" class="diagnostics-close" data-diagnostics-close="true" aria-label="Close diagnostics">×</button>
      </div>
      <pre id="diagnosticsModalBody" class="diagnostics-modal-body"></pre>
    </div>
  `,t.addEventListener(`click`,e=>{let n=e.target;(n===t||n.closest(`[data-diagnostics-close='true']`))&&Je()}),document.body.appendChild(t),t}function qe(e,t){let n=Ke(),r=n.querySelector(`#diagnosticsModalTitle`),i=n.querySelector(`#diagnosticsModalBody`);r&&(r.textContent=e.trim()||`Status details`),i&&(i.textContent=t.trim()),n.classList.remove(`hidden`),document.body.classList.add(`diagnostics-open`)}function Je(){let e=document.getElementById(`diagnosticsModal`);e&&(e.classList.add(`hidden`),document.body.classList.remove(`diagnostics-open`))}function Ye(e){let t=e.partition?.manifest?.spec?.reconciliation?.mode??`manual`,n=e.partition?.manifest?.spec?.labels?.managedBy??``,r=(e.intents??[]).some(e=>e.targetSummary&&e.targetSummary!==`Unassigned`),i=e.health?.pending??0,a=[];return a.push(`${i} asset${i===1?` is`:`s are`} in <strong>Planned</strong> state — no reconcile has run yet.`),n===`external-compose`&&a.push(`Resources in this partition are managed externally by Docker Compose.`),t===`manual`?r?a.push(`Click <strong>Reconcile now</strong> in the sidebar to run the first reconcile and deploy assets.`):a.push(`No pusher is assigned. Assets will stay in Planned state until a pusher is configured.`):a.push(`The reconciler will process these assets automatically in the next cycle.`),`<div class="info-callout mt-2"><span class="info-callout-icon">?</span><div><strong>Why is this partition Progressing?</strong><p>${a.join(` `)}</p></div></div>`}function Xe(e){let t=[`component`,`role`,`stack`,`topology`],n=[];return t.forEach(t=>{e[t]&&n.push(e[t])}),[...new Set(n)]}function Ze(e){return(y.detail?.intents??[]).find(t=>t.name===e)??null}function Qe(e,t){return Ze(e)?.assets?.find(e=>e.name===t)??null}var W={partition:`#F0E442`,intent:`#CC79A7`,runtime:`#0072B2`,config:`#009E73`,storage:`#56B4E9`,traffic:`#D55E00`,muted:`#8B949E`};function $e(e){return e.kind===`partition`?W.partition:e.kind===`intent`?W.intent:ut(e.assetType??e.kind)}function et(e){return e.kind===`partition`?`◫`:e.kind===`intent`?`⊞`:lt(e.assetType??e.kind)}function tt(e){return e.kind===`partition`?`${e.meta?.reconciliation??`manual`} reconcile · ${e.meta?.deletionPolicy??`orphan`} delete`:e.kind===`intent`?e.meta?.target??e.displayStatus??`Intent`:`${Q(e.assetType??e.kind)} · ${e.displayStatus??`Asset`}`}var G=[`Compute`,`Network`,`Config`,`Storage`,`Operations`],nt={Compute:W.runtime,Volume:W.storage,Config:W.config,ObjectStore:W.storage,Database:W.traffic,SQLDatabase:W.traffic,LoadBalancer:W.traffic,Observability:W.config},rt={Image:`Container image reference (registry/name:tag@digest)`,Scale:`Desired replica count`,Env:`Environment variables injected at runtime`,Config:`ConfigMap or file mounts`,Storage:`Persistent volume mounts`,Ports:`Exposed service ports`,Port:`Service listener port`,Health:`Health check probe is configured`,CPU:`CPU resource limit/request`,Memory:`Memory resource limit/request`,Engine:`Storage engine or database type`,Version:`Engine version`,Database:`Database name`,Mode:`Deployment or storage mode`,Endpoint:`Connection endpoint address`,Size:`Volume storage capacity`,Access:`Volume access mode (e.g. ReadWriteOnce)`,Format:`Config file format (yaml / json / env)`,Files:`Config file definitions`,Inline:`Config data is defined inline in the manifest`,Targets:`Number of load balancer backend targets`,Listeners:`Number of load balancer listeners`,Buckets:`Object storage bucket names`,Provider:`Observability provider`,Receivers:`Telemetry input protocols`,Exporters:`Telemetry export destinations`,Outputs:`Output keys exposed to dependent intents`},it={Scale:`fact-scale`,Ports:`fact-port`,Port:`fact-port`,CPU:`fact-resource`,Memory:`fact-resource`,Env:`fact-env`,Storage:`fact-storage`,Size:`fact-storage`,Engine:`fact-engine`,Version:`fact-engine`,Outputs:`fact-outputs`};function at(e){return e===`Release`?`fact-release`:it[e]?` ${it[e]}`:``}function ot(e){return rt[e]??e}function K(e){return(y.catalog?.assetTypes??[]).find(t=>t.type===e)??null}function q(e){return e.replace(/\[\d+\]/g,`[]`)}function J(e,t){let n=q(t),r=n.replace(/\[\]/g,``).split(`.`)[0];return(e??[]).find(e=>e.path===n||e.path===r)??null}function st(e,t){let n=K(e?.type??``);return J(e?.hints,t)??J(n?.hints,t)??J(n?.fields,t)??null}function Y(e,t=``){if(e==null||e===``)return[];if(Array.isArray(e))return e.length?e.flatMap((e,n)=>Y(e,`${t}[${n}]`)):t?[{path:t,value:`[]`}]:[];if(typeof e==`object`){let n=Object.entries(e);return n.length?n.flatMap(([e,n])=>Y(n,t?`${t}.${e}`:e)):t?[{path:t,value:`{}`}]:[]}return t?[{path:t,value:String(e)}]:[]}function X(e,t={}){let n=t.limit??16,r=t.truncateAt??48,o=K(e?.type??``),c=new Map;(o?.fields??[]).forEach((e,t)=>{c.set(e.path,t)});let l=Y(e?.properties??{}).sort((e,t)=>{let n=q(e.path),r=q(t.path),i=n.replace(/\[\]/g,``).split(`.`)[0],a=r.replace(/\[\]/g,``).split(`.`)[0],o=c.get(n)??c.get(i)??2**53-1,s=c.get(r)??c.get(a)??2**53-1;return o===s?e.path.localeCompare(t.path):o-s});if(!l.length)return``;let u=l.slice(0,n),d=u.map(t=>{let n=st(e,t.path);return`<span class="fact" title="${a([t.path,n?.title,n?.description].filter(Boolean).join(` - `))}">${i(`${t.path}: ${s(t.value,r)}`)}</span>`}).join(``);if(l.length===u.length)return d;let f=l.length-u.length;return`${d}<span class="fact" title="${f} more properties available">+${f} more</span>`}function ct(e,t=[],n={}){let r=Object.entries(e??{}),o=n.limit??r.length,c=n.truncateAt??2**53-1,l=r.slice(0,o).map(([e,n])=>{let r=J(t,`outputs.${e}`);return`<span class="fact" title="${a([`outputs.${e}`,r?.title,r?.description].filter(Boolean).join(` - `))}">${i(`${e}: ${s(String(n),c)}`)}</span>`}).join(``);if(r.length<=o)return l;let u=r.length-o;return`${l}<span class="fact" title="${u} more outputs available">+${u} more</span>`}function Z(e){return K(e)?.category??`Other`}function Q(e){return K(e)?.title??o(e)}function lt(e){return K(e)?.icon??`⬡`}function ut(e){return nt[e]??W.muted}function dt(e){let t={Compute:W.runtime,Network:W.traffic,Config:W.config,Storage:W.storage,Operations:W.config,Other:W.muted};return t[e]??t.Other}function ft(e){let t=new Map;return e.forEach(e=>{let n=Z(e.type);t.set(n,(t.get(n)??0)+1)}),[...t.keys()].sort((e,t)=>{let n=G.indexOf(e),r=G.indexOf(t);return n===-1&&r===-1?e.localeCompare(t):n===-1?1:r===-1?-1:n-r}).map(e=>({category:e,count:t.get(e)}))}function pt(e){let t=new Map;return e.forEach(e=>{let n=Z(e.type);t.has(n)||t.set(n,[]),t.get(n).push(e)}),[...t.keys()].sort((e,t)=>{let n=G.indexOf(e),r=G.indexOf(t);return n===-1&&r===-1?e.localeCompare(t):n===-1?1:r===-1?-1:n-r}).map(e=>({category:e,assets:t.get(e).sort((e,t)=>e.name.localeCompare(t.name))}))}function mt(){document.querySelectorAll(`[data-tab-target]`).forEach(e=>{e.addEventListener(`click`,()=>fe(e.dataset.tabTarget))}),document.getElementById(`partitionSearch`)?.addEventListener(`input`,M),document.getElementById(`refreshButton`)?.addEventListener(`click`,()=>E(!0).catch($)),document.getElementById(`reconcileButton`)?.addEventListener(`click`,ht),document.getElementById(`createPartitionButton`)?.addEventListener(`click`,()=>d(`Create partition via guardianctl partition push`,`success`)),document.getElementById(`overviewCreatePartitionButton`)?.addEventListener(`click`,()=>d(`Create partition via guardianctl partition push`,`success`)),document.getElementById(`appGridSearch`)?.addEventListener(`input`,N),document.getElementById(`historyFilter`)?.addEventListener(`input`,z),document.getElementById(`historyGroupToggle`)?.addEventListener(`change`,z),document.getElementById(`historyApply`)?.addEventListener(`click`,()=>ve().catch($)),document.getElementById(`historyReset`)?.addEventListener(`click`,()=>ye().catch($)),document.getElementById(`refreshRolloutsButton`)?.addEventListener(`click`,()=>{A(!0).catch($),T()}),[`showContainEdges`,`showJoinEdges`,`showAssetEdges`,`showOutputEdges`].forEach(e=>{document.getElementById(e)?.addEventListener(`change`,V)}),document.getElementById(`topologyZoomOut`)?.addEventListener(`click`,()=>gt(-.1)),document.getElementById(`topologyZoomIn`)?.addEventListener(`click`,()=>gt(.1)),document.getElementById(`topologyResetLayout`)?.addEventListener(`click`,()=>{y.topology.nodePositions={},V()}),document.addEventListener(`click`,e=>{let t=e.target.closest(`[data-diagnostic-detail]`);t&&(e.preventDefault(),qe(t.dataset.diagnosticTitle??`Status details`,t.dataset.diagnosticDetail??`No diagnostic details were provided.`))}),document.addEventListener(`keydown`,e=>{e.key===`Escape`&&Je()}),Ke()}async function ht(){let e=y.selectedPartition;if(!e){d(`Select a partition first.`,`error`);return}await t(`/api/partitions/${encodeURIComponent(e)}/reconcile`,{method:`POST`}),ue(),d(`Reconciliation requested.`,`success`),await E(!1),await D(e,!1)}function gt(e){let t=document.getElementById(`topologyCanvas`),n=vt(y.topology.zoom,.4,2.5),r=vt(Math.round((n+e)*100)/100,.4,2.5);if(n===r)return;let i=t?t.scrollLeft+t.clientWidth/2:0,a=t?t.scrollTop+t.clientHeight/2:0;if(y.topology.zoom=r,V(),t){let e=r/n;t.scrollLeft=Math.max(0,i*e-t.clientWidth/2),t.scrollTop=Math.max(0,a*e-t.clientHeight/2)}_t()}function _t(){let e=y.topology.zoom,t=document.getElementById(`topologyZoomOut`),n=document.getElementById(`topologyZoomIn`),r=document.getElementById(`topologyZoomValue`);r&&(r.textContent=`${Math.round(e*100)}%`),t&&(t.disabled=e<=.4),n&&(n.disabled=e>=2.5)}function vt(e,t,n){return Math.min(n,Math.max(t,e))}function $(e){d(e?.message??`Unexpected error`,`error`)}async function yt(){try{y.catalog=await t(`/api/catalog`)}catch{}}yt();