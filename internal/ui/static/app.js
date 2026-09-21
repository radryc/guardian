(function(){let e=document.createElement(`link`).relList;if(e&&e.supports&&e.supports(`modulepreload`))return;for(let e of document.querySelectorAll(`link[rel="modulepreload"]`))n(e);new MutationObserver(e=>{for(let t of e)if(t.type===`childList`)for(let e of t.addedNodes)e.tagName===`LINK`&&e.rel===`modulepreload`&&n(e)}).observe(document,{childList:!0,subtree:!0});function t(e){let t={};return e.integrity&&(t.integrity=e.integrity),e.referrerPolicy&&(t.referrerPolicy=e.referrerPolicy),e.crossOrigin===`use-credentials`?t.credentials=`include`:e.crossOrigin===`anonymous`?t.credentials=`omit`:t.credentials=`same-origin`,t}function n(e){if(e.ep)return;e.ep=!0;let n=t(e);fetch(e.href,n)}})();function e(){return{activePanel:`overviewPanel`,selectedPartition:``,overview:null,detail:null,expandedAssetKey:``,expandedRolloutKeys:{},history:null,historyLoading:!1,historyError:``,rollouts:null,rolloutsLoading:!1,rolloutsError:``,catalog:null,topology:{zoom:1,nodePositions:{},selectedNodeId:``},activityDrawer:{intentName:``,data:null,loading:!1,error:``},historyOptions:{limit:10,since:``,until:``},refreshTimer:void 0,rolloutsRefreshTimer:void 0,fastRefreshUntil:0,refreshIntervalMs:2e4,diagnosticDetails:{}}}async function t(e,t){let n=await fetch(e,{headers:{Accept:`application/json`,...t?.headers??{}},...t});if(!n.ok){let e=`HTTP ${n.status}`;try{let t=await n.json();e=t.error??t.message??e}catch{}throw Error(e)}return n.status===204?null:n.json()}function n(e,t){let n=document.getElementById(e);n&&(n.textContent=String(t))}function r(e){let t=document.getElementById(`syncIndicator`);t&&(t.textContent=e)}function i(e){return e.replace(/&/g,`&amp;`).replace(/</g,`&lt;`).replace(/>/g,`&gt;`).replace(/"/g,`&quot;`).replace(/'/g,`&#x27;`)}function a(e){return e.replace(/[^a-zA-Z0-9_-]/g,e=>`&#${e.charCodeAt(0)};`)}function o(e){return e.replace(/[-_]/g,` `).replace(/([a-z])([A-Z])/g,`$1 $2`).replace(/\b\w/g,e=>e.toUpperCase())}function s(e,t){return e.length<=t?e:`${e.slice(0,t-1)}…`}function c(e){if(!e||e===`0001-01-01T00:00:00Z`)return`—`;let t=new Date(e),n=Math.floor((Date.now()-t.getTime())/1e3);return n<60?`${n}s ago`:n<3600?`${Math.floor(n/60)}m ago`:n<86400?`${Math.floor(n/3600)}h ago`:t.toLocaleDateString(void 0,{month:`short`,day:`numeric`,year:`numeric`})}function l(e){if(!e)return``;let t=new Date(e);if(Number.isNaN(t.getTime()))return``;let n=e=>String(e).padStart(2,`0`);return`${t.getFullYear()}-${n(t.getMonth()+1)}-${n(t.getDate())}T${n(t.getHours())}:${n(t.getMinutes())}`}function u(e){let t=e.trim();if(!t)return``;let n=new Date(t);return Number.isNaN(n.getTime())?``:n.toISOString()}function d(e,t=`info`){let n=document.getElementById(`toastContainer`);if(!n)return;let r=document.createElement(`div`);r.className=`toast toast-${t}`,r.setAttribute(`role`,`status`),r.setAttribute(`aria-live`,`polite`),r.textContent=e,n.appendChild(r),requestAnimationFrame(()=>r.classList.add(`toast-visible`)),setTimeout(()=>{r.classList.remove(`toast-visible`),r.addEventListener(`transitionend`,()=>r.remove(),{once:!0}),setTimeout(()=>r.remove(),400)},3600)}var f={partition:`#F0E442`,intent:`#CC79A7`,runtime:`#0072B2`,config:`#009E73`,storage:`#56B4E9`,traffic:`#D55E00`,muted:`#8B949E`},p={healthy:`#00C369`,attention:`#FCB519`,failing:`#EE5F54`,pending:`#00ADE4`,drifted:`#FCB519`,"drifted-locked":`#F5A623`,neutral:`#566778`};function m(e){return e.kind===`partition`?200:e.kind===`intent`?230:220}function h(e){return e.kind===`partition`?76:72}function ee(e){return e.kind===`partition`?f.partition:e.kind===`intent`?f.intent:{Compute:f.runtime,Volume:f.storage,Config:f.config,ObjectStore:f.storage,Database:f.traffic,SQLDatabase:f.traffic,LoadBalancer:f.traffic,Observability:f.config}[e.assetType??``]??f.muted}function te(e){return e.kind===`partition`?`◫`:e.kind===`intent`?`⊞`:{Compute:`⧖`,Volume:`⊠`,Config:`≡`,ObjectStore:`⬜`,Database:`⫿`,SQLDatabase:`⫿`,LoadBalancer:`⊷`,Observability:`◎`}[e.assetType??``]??`⬡`}function ne(e){return e.kind===`partition`?`${e.meta?.reconciliation??`manual`} reconcile · ${e.meta?.deletionPolicy??`orphan`} delete`:e.kind===`intent`?e.meta?.target??e.displayStatus??`Intent`:`${e.assetType??`Asset`} · ${e.displayStatus??`Asset`}`}function re(e){return p[e.health??e.status??`neutral`]??p.neutral}function ie(e,t){let n=e.find(e=>e.kind===`partition`),r=e.filter(e=>e.kind===`intent`).sort((e,t)=>Number(e.level)-Number(t.level)||e.label.localeCompare(t.label)),i=e.filter(e=>e.kind===`asset`).sort((e,t)=>(e.parentID??``).localeCompare(t.parentID??``)||Number(e.level)-Number(t.level)||e.label.localeCompare(t.label)),a={},o=new Map;r.forEach(e=>{let t=Number(e.level??1);o.has(t)||o.set(t,[]),o.get(t).push(e)});let s=70;if([...o.keys()].sort((e,t)=>e-t).forEach(e=>{o.get(e).forEach(t=>{a[t.id]={x:260+(e-1)*320,y:s},s+=154}),s+=24}),n){let e=r.map(e=>a[e.id]).filter(Boolean),t=e.length?Math.min(...e.map(e=>e.y)):90,i=e.length?Math.max(...e.map(e=>e.y)):90;a[n.id]={x:40,y:Math.round((t+i)/2)}}r.forEach(e=>{let t=a[e.id];if(!t)return;let n=new Map;i.filter(t=>t.parentID===e.id).forEach(t=>{let r=Math.max(0,Number(t.level??0)-Number(e.level??0)-2);n.has(r)||n.set(r,[]),n.get(r).push(t)}),[...n.keys()].sort((e,t)=>e-t).forEach(e=>{let r=n.get(e),i=Math.max(0,(r.length-1)*96);r.forEach((n,r)=>{a[n.id]={x:t.x+280+e*250,y:t.y-i/2+r*96+e*8}})})});let c=Math.min(...Object.values(a).map(e=>e.y),40);if(c<40){let e=40-c;Object.values(a).forEach(t=>{t.y+=e})}return Object.entries(t).forEach(([e,t])=>{a[e]&&(a[e]={x:t.x,y:t.y})}),a}function ae(e,t,n,r){let i=e.x+m(n),a=e.y+h(n)/2,o=t.x,s=t.y+h(r)/2,c=o-i,l=c>=0?1:-1,u=Math.max(70,Math.abs(c)/2);return`M ${i} ${a} C ${i+u*l} ${a}, ${o-u*l} ${s}, ${o} ${s}`}function oe(e,t,n,r){e.querySelectorAll(`path.topology-edge`).forEach((e,i)=>{let a=n[i];if(!a)return;let o=t[a.from],s=t[a.to],c=r.get(a.from),l=r.get(a.to);o&&s&&c&&l&&e.setAttribute(`d`,ae(o,s,c,l))})}function se(e){let{canvas:t,topology:n,zoom:r,savedPositions:o,selectedNodeId:c,filters:l,onSelectNode:u,onDragNode:d}=e;if(!n?.nodes?.length){t.innerHTML=`<p class="empty-state" style="padding:24px">Select a partition to visualize its topology.</p>`;return}let f=n.nodes,p=new Map(f.map(e=>[e.id,e])),se=(n.edges??[]).filter(e=>l[e.kind]!==!1),g=ie(f,o),ce=p.get(c)??f.find(e=>e.kind===`intent`)??f[0],le=Math.max(...Object.values(g).map(e=>e.x+260),400),_=Math.max(...Object.values(g).map(e=>e.y+100),260),v=le+40,y=_+40,b=[`<div class="topology-svg-frame">`,`<svg class="topology-svg" viewBox="0 0 ${v} ${y}" width="${Math.round(v*r)}" height="${Math.round(y*r)}" xmlns="http://www.w3.org/2000/svg">`,`<defs><filter id="ts"><feDropShadow dx="0" dy="6" stdDeviation="10" flood-color="rgba(0,0,0,0.28)"/></filter></defs>`];se.forEach(e=>{let t=g[e.from],n=g[e.to],r=p.get(e.from),o=p.get(e.to);if(!(!t||!n||!r||!o)&&(b.push(`<path class="topology-edge ${a(e.kind)}" d="${ae(t,n,r,o)}" />`),e.label)){let a=t.x+m(r),s=t.y+h(r)/2,c=n.x,l=n.y+h(o)/2;b.push(`<text class="topology-edge-label" x="${(a+c)/2}" y="${(s+l)/2-10}">${i(e.label)}</text>`)}}),f.forEach(e=>{let t=g[e.id];if(!t)return;let n=ee(e),r=ce?.id===e.id,o=m(e),c=h(e);b.push(`
      <g class="topology-node ${a(e.kind)}${r?` selected`:``}" data-node="${a(e.id)}" transform="translate(${t.x},${t.y})">
        <rect class="topology-node-card" width="${o}" height="${c}" rx="12" filter="url(#ts)" />
        <rect class="topology-node-accent" width="4" height="${c}" rx="4" fill="${n}" />
        <circle cx="18" cy="18" r="5.5" fill="${re(e)}" />
        <text x="32" y="20" class="topology-node-title">${i(`${te(e)} ${e.label}`)}</text>
        <text x="32" y="38" class="topology-node-subtitle">${i(ne(e))}</text>
        <text x="14" y="60" class="topology-node-description">${i(s(e.description??``,68))}</text>
      </g>
    `)}),b.push(`</svg>`,`</div>`),t.innerHTML=b.join(``);let x=t.querySelector(`svg.topology-svg`);x.querySelectorAll(`[data-node]`).forEach(e=>{let t=e.dataset.node;function n(e,t){let n=x.createSVGPoint();n.x=e,n.y=t;let r=x.getScreenCTM();if(!r)return{x:e,y:t};let i=n.matrixTransform(r.inverse());return{x:i.x,y:i.y}}let r=!1,i=!1,a=0,o=0,s=0,c=0;e.addEventListener(`pointerdown`,n=>{if(n.button!==0)return;r=!0,i=!1,a=n.clientX,o=n.clientY;let l=g[t];s=l?l.x:0,c=l?l.y:0,e.setPointerCapture(n.pointerId),n.stopPropagation()}),e.addEventListener(`pointermove`,l=>{if(!r)return;let u=n(a,o),f=n(l.clientX,l.clientY),m=s+(f.x-u.x),h=c+(f.y-u.y);!i&&Math.abs(m-s)<3&&Math.abs(h-c)<3||(i=!0,g[t]={x:m,y:h},e.setAttribute(`transform`,`translate(${m},${h})`),e.classList.add(`dragging`),oe(x,g,se,p),d(t,{...g}),l.stopPropagation())}),e.addEventListener(`pointerup`,n=>{r&&(r=!1,e.releasePointerCapture(n.pointerId),e.classList.remove(`dragging`),i&&d(t,{...g}),n.stopPropagation())}),e.addEventListener(`click`,e=>{if(i){e.stopPropagation(),e.preventDefault();return}u(t,{...g})})})}function g(e){e&&(e.innerHTML=`
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
  `)}var ce=3e3,le=200,_=()=>void 0,v=[],y=[],b=``,x=null,S=null,C={},w,T=!1,ue=!1;function de(e){_=e,document.getElementById(`scanStartButton`)?.addEventListener(`click`,()=>be().catch(_)),document.getElementById(`scanGenerateButton`)?.addEventListener(`click`,()=>Ae().catch(_)),document.getElementById(`scanList`)?.addEventListener(`click`,e=>{let t=e.target.closest(`[data-scan-select]`);t&&xe(t.dataset.scanSelect??``).catch(_)}),document.getElementById(`scanBundleResult`)?.addEventListener(`change`,e=>{let t=e.target;if(!(t instanceof HTMLInputElement))return;let n=t.dataset.intentToggle;n&&(C[n]=t.checked,je())}),document.getElementById(`scanBundleResult`)?.addEventListener(`click`,e=>{let t=e.target.closest(`[data-scan-save]`);t&&Ne(t.dataset.scanSave===`reconcile`).catch(_)})}function fe(){if(T=!document.getElementById(`scanPanel`)?.classList.contains(`hidden`),!T){pe();return}v.length===0&&y.length===0?E().catch(_):ye(),me()}function pe(){w!==void 0&&(window.clearTimeout(w),w=void 0)}function me(){if(w!==void 0)return;let e=async()=>{if(w=void 0,T){try{await E(),b&&ge()&&await Se()}catch{}T&&he()&&(w=window.setTimeout(e,ce))}};he()&&(w=window.setTimeout(e,ce))}function he(){return y.some(e=>e.status===`Queued`||e.status===`Running`)}function ge(){return!x||x.status===`Queued`||x.status===`Running`}async function E(){let e=await t(`/api/scans`);v=(e.pushers??[]).filter(e=>e.includes(`aws`)),y=e.scans??[],_e(),ye()}function _e(){let e=document.getElementById(`scanPusher`);if(!e)return;let t=e.value;e.innerHTML=v.length===0?`<option value="">No AWS pushers configured</option>`:v.map(e=>`<option value="${a(e)}">${i(e)}</option>`).join(``),t&&v.includes(t)&&(e.value=t)}function ve(e){let t=String(e??``).toLowerCase();return t===`succeeded`?`badge badge-healthy`:t===`failed`?`badge badge-failing`:t===`running`?`badge badge-pending`:`badge badge-neutral`}function ye(){let e=document.getElementById(`scanList`);if(e){if(y.length===0){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No scans yet. Start one above.`;return}e.className=`grid gap-1.5`,e.innerHTML=y.map(e=>{let t=e.summary??{},n=(e.regions??[]).join(`, `)||`—`,r=e.createdAt?c(e.createdAt):`—`,o=[`${t.bucketCount??0} buckets`,`${t.serviceCount??0} services`,`${t.loadBalancerCount??0} LBs`,`${t.stackCount??0} stacks`,`${t.inventoryCount??0} inventory`].join(` · `);return`
      <div class="flex items-center gap-3 px-3.5 py-2.5 rounded-lg border ${e.scanID===b?`border-[#00ADE4]/50 bg-[#00ADE4]/[0.06]`:`border-white/[0.07] bg-[#0D1220]`}">
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2 flex-wrap">
            <span class="text-[13px] font-semibold text-[#E5ECF4] truncate">${i(e.pusher)}</span>
            <span class="${ve(e.status)}">${i(e.status??``)}</span>
            <span class="text-[11px] text-[#566778]">${i(r)}</span>
          </div>
          <div class="text-[12px] text-[#9BB0CF] mt-0.5 truncate">${i(n)} · ${i(o)}</div>
        </div>
        <button class="btn-ghost shrink-0" data-scan-select="${a(e.scanID)}">${e.status===`Succeeded`?`Inspect`:`View`}</button>
      </div>`}).join(``)}}async function be(){let e=document.getElementById(`scanPusher`)?.value?.trim()??``;if(!e){d(`Select an AWS pusher first.`,`error`);return}let n=document.getElementById(`scanRegions`)?.value?.trim()??``,r=document.getElementById(`scanIncludeTypes`)?.value?.trim()??``,i=document.getElementById(`scanExcludeTypes`)?.value?.trim()??``,a={pusher:e,inventory:document.getElementById(`scanInventory`)?.checked??!1};n&&(a.regions=n.split(`,`).map(e=>e.trim()).filter(Boolean)),r&&(a.includeResourceTypes=r.split(`,`).map(e=>e.trim()).filter(Boolean)),i&&(a.excludeResourceTypes=i.split(`,`).map(e=>e.trim()).filter(Boolean)),b=(await t(`/api/scans`,{method:`POST`,headers:{"Content-Type":`application/json`},body:JSON.stringify(a)})).scanID??``,x=null,S=null,d(`Scan queued. The AWS pusher will pick it up shortly.`,`success`),await E(),me()}async function xe(e){e&&(b=e,S=null,C={},await Se(),ye())}async function Se(){b&&(x=await t(`/api/scans/${encodeURIComponent(b)}`),Ce())}function D(e,t){return`<span class="pill">${i(e)} <strong>${t??0}</strong></span>`}function Ce(){let e=document.getElementById(`scanDetailWrap`);if(!e||!x)return;let t=x.status===`Succeeded`||x.status===`Failed`;if(e.classList.toggle(`hidden`,!t),!t){let e=document.getElementById(`scanDetailTitle`);e&&(e.textContent=`Scan ${b} is ${x.status??`pending`}`);return}let n=document.getElementById(`scanDetailTitle`);n&&(n.textContent=`Scan ${b}`);let r=document.getElementById(`scanDetailSubtitle`);r&&(r.textContent=[x.account?`account ${x.account}`:``,Array.isArray(x.regions)?x.regions.join(`, `):``].filter(Boolean).join(` · `));let i=x.summary??{},a=document.getElementById(`scanSummary`);a&&(a.innerHTML=[D(`Regions`,i.regionCount),D(`Buckets`,i.bucketCount),D(`EFS`,i.fileSystemCount),D(`Params`,i.parameterCount),D(`Secrets`,i.secretCount),D(`Services`,i.serviceCount),D(`LBs`,i.loadBalancerCount),D(`Stacks`,i.stackCount),D(`Inventory`,i.inventoryCount),D(`Managed`,i.managedCount),D(`Errors`,i.errorCount)].join(``)),we(),Ee(),Oe(),ke()}function we(){let e=document.getElementById(`scanManagedList`);if(!e)return;let t=Te(x);if(t.length===0){e.innerHTML=``;return}e.innerHTML=`
    <div class="text-[11px] font-bold uppercase tracking-[0.09em] text-[#566778] mb-1.5">Already managed (${t.length})</div>
    <div class="grid gap-1">
      ${t.slice(0,50).map(e=>`
        <div class="flex items-center gap-2 text-[12px] text-[#9BB0CF] px-3 py-1.5 rounded border border-white/[0.07] bg-[#0D1220]">
          <span class="badge badge-neutral">${i(e.kind??``)}</span>
          <span class="text-[#E5ECF4] truncate">${i(e.identifier??``)}</span>
          <span class="text-[#566778] truncate ml-auto">${i(e.partition?`${e.partition}/${e.intent}/${e.asset??``}`:e.existingIntent?`referenced by existing intent`:``)}</span>
        </div>`).join(``)}
      ${t.length>50?`<div class="text-[12px] text-[#566778] px-3">+${t.length-50} more</div>`:``}
    </div>`}function Te(e){let t=[],n=(e,n,r)=>{for(let i of n??[])i?.managed?.managed&&t.push({kind:e,identifier:r(i),partition:i.managed.partition,intent:i.managed.intent,asset:i.managed.asset})};return n(`bucket`,e?.buckets,e=>e.name),n(`fileSystem`,e?.fileSystems,e=>e.id),n(`parameter`,e?.parameters,e=>e.name),n(`secret`,e?.secrets,e=>e.name),n(`service`,e?.services,e=>`${e.clusterName??``}/${e.name??``}`),n(`loadBalancer`,e?.loadBalancers,e=>e.name),n(`stack`,e?.stacks,e=>e.name),t}function Ee(){let e=document.getElementById(`scanUnmappedList`);if(!e)return;let t=De(x);if(t.length===0){e.innerHTML=``;return}e.innerHTML=`
    <div class="text-[11px] font-bold uppercase tracking-[0.09em] text-[#566778] mb-1.5">Foreign CloudFormation stacks (${t.length})</div>
    <div class="grid gap-1">
      ${t.slice(0,25).map(e=>`
        <div class="flex items-center gap-2 text-[12px] text-[#9BB0CF] px-3 py-1.5 rounded border border-white/[0.07] bg-[#0D1220]">
          <span class="badge badge-attention">stack</span>
          <span class="text-[#E5ECF4] truncate">${i(e.name??``)}</span>
          <span class="text-[#566778] truncate ml-auto">${i(e.region??``)} · ${i(e.status??``)}</span>
        </div>`).join(``)}
      ${t.length>25?`<div class="text-[12px] text-[#566778] px-3">+${t.length-25} more</div>`:``}
    </div>`}function De(e){return(e?.stacks??[]).filter(e=>!e?.managed?.managed)}function Oe(){let e=document.getElementById(`scanInventoryList`);if(!e)return;let t=x?.inventory??{},n=Object.keys(t).sort();if(n.length===0){e.innerHTML=``;return}e.innerHTML=`
    <div class="text-[11px] font-bold uppercase tracking-[0.09em] text-[#566778] mb-1.5">Inventory (not importable)</div>
    <div class="grid gap-1.5">
      ${n.map(e=>{let n=t[e]??{},r=Object.keys(n).sort();return r.length===0?``:`
          <details class="rounded border border-white/[0.07] bg-[#0D1220]">
            <summary class="px-3 py-2 text-[12px] text-[#E5ECF4] cursor-pointer select-none">${i(e)} <span class="text-[#566778]">(${r.length} types)</span></summary>
            <div class="px-3 pb-2.5 grid gap-1">
              ${r.map(e=>{let t=n[e]??[];if(t.length===0)return``;let r=t.slice(0,le);return`
                  <details class="rounded border border-white/[0.06] bg-[#151B2B]">
                    <summary class="px-2.5 py-1.5 text-[12px] text-[#9BB0CF] cursor-pointer select-none">${i(e)} <span class="text-[#566778]">(${t.length})</span></summary>
                    <div class="px-2.5 pb-2 grid gap-0.5">
                      ${r.map(e=>`<div class="text-[11px] text-[#9BB0CF] truncate">${i(e.identifier??``)}</div>`).join(``)}
                      ${t.length>r.length?`<div class="text-[11px] text-[#566778]">+${t.length-r.length} more</div>`:``}
                    </div>
                  </details>`}).join(``)}
            </div>
          </details>`}).join(``)}
    </div>`}function ke(){let e=document.getElementById(`scanErrorsList`);if(!e)return;let t=x?.errors??[];if(t.length===0){e.innerHTML=``;return}e.innerHTML=`
    <div class="text-[11px] font-bold uppercase tracking-[0.09em] text-[#566778] mb-1.5">Scan errors (${t.length})</div>
    <div class="grid gap-1 max-h-52 overflow-y-auto">
      ${t.slice(0,100).map(e=>`
        <div class="text-[12px] text-[#9BB0CF] px-3 py-1.5 rounded border border-[#E5484D]/25 bg-[#E5484D]/[0.06]">
          ${i([e.region,e.resourceType].filter(Boolean).join(` · `))} — ${i(e.message??``)}
        </div>`).join(``)}
      ${t.length>100?`<div class="text-[12px] text-[#566778] px-3">+${t.length-100} more</div>`:``}
    </div>`}async function Ae(){if(!b){d(`Select a finished scan first.`,`error`);return}let e=document.getElementById(`scanPartitionName`)?.value?.trim()??``;if(!e){d(`Enter a target partition name.`,`error`);return}let n=document.getElementById(`scanGrouping`)?.value??`stack`,r=new URLSearchParams({partition:e,grouping:n}),i=await t(`/api/scans/${encodeURIComponent(b)}/bundle?${r.toString()}`);S=i,C={};for(let e of i.bundle?.intents??[])C[e.manifest?.metadata?.name??``]=!0;je()}function je(){let e=document.getElementById(`scanBundleResult`);if(!e)return;if(!S){e.innerHTML=``;return}let t=S.draft??{},n=t.warnings??[],r=t.managed??[],o=S.bundle?.intents??[];if(o.length===0){e.innerHTML=`
      <div class="empty-state text-sm text-[#566778]">
        Nothing new to import${r.length>0?` — ${r.length} scanned resources are already managed or imported.`:`.`}
      </div>`;return}let s=o.filter(e=>C[e.manifest?.metadata?.name??``]).length;e.innerHTML=`
    ${n.length>0?`
      <div class="grid gap-1 mb-3">
        ${n.map(e=>`<div class="text-[12px] text-[#FCB519] px-3 py-1.5 rounded border border-[#FCB519]/25 bg-[#FCB519]/[0.06]">${i(e)}</div>`).join(``)}
      </div>`:``}
    <div class="grid gap-1.5 mb-3">
      ${o.map(e=>{let t=e.manifest??{},n=t.metadata?.name??``,r=t.spec?.target?.region??``,o=t.spec?.assets??[],s=C[n]??!1;return`
          <label class="flex items-start gap-2.5 px-3.5 py-2.5 rounded-lg border ${s?`border-[#00ADE4]/50 bg-[#00ADE4]/[0.06]`:`border-white/[0.07] bg-[#0D1220]`} cursor-pointer">
            <input type="checkbox" ${s?`checked`:``} data-intent-toggle="${a(n)}" class="accent-[#00ADE4] mt-0.5" />
            <span class="min-w-0 flex-1">
              <span class="flex items-center gap-2 flex-wrap">
                <span class="text-[13px] font-semibold text-[#E5ECF4]">${i(n)}</span>
                <span class="badge badge-neutral">${i(r)}</span>
              </span>
              <span class="flex gap-1 flex-wrap mt-1">
                ${o.map(e=>`<span class="pill">${i(e.type??``)} · ${i(e.name??``)}</span>`).join(``)}
              </span>
            </span>
          </label>`}).join(``)}
    </div>
    <details class="rounded border border-white/[0.07] bg-[#0D1220] mb-3">
      <summary class="px-3 py-2 text-[12px] text-[#E5ECF4] cursor-pointer select-none">Manifest preview (selected: ${s}/${o.length})</summary>
      <pre class="px-3 pb-3 text-[11px] text-[#9BB0CF] overflow-x-auto whitespace-pre">${i(Me(o))}</pre>
    </details>
    <div class="flex gap-2 flex-wrap">
      <button class="btn-primary" data-scan-save="reconcile" ${s===0?`disabled`:``}>Save &amp; reconcile</button>
      <button class="btn-secondary" data-scan-save="save" ${s===0?`disabled`:``}>Save only</button>
    </div>`}function Me(e){let t=e.filter(e=>C[e.manifest?.metadata?.name??``]);return JSON.stringify(t.map(e=>e.manifest),null,2)}async function Ne(e){if(!S||ue)return;let n=document.getElementById(`scanPartitionName`)?.value?.trim()??``;if(!n){d(`Enter a target partition name.`,`error`);return}let r=(S.bundle?.intents??[]).filter(e=>C[e.manifest?.metadata?.name??``]);if(r.length===0){d(`Select at least one intent.`,`error`);return}ue=!0;try{await t(`/api/partitions/${encodeURIComponent(n)}/bundle`,{method:`PUT`,headers:{"Content-Type":`application/json`},body:JSON.stringify({partition:S.bundle.partition,intents:r,removeMissingIntents:!1})}),e&&await t(`/api/partitions/${encodeURIComponent(n)}/reconcile`,{method:`POST`}),d(`Bundle saved to partition ${n}.`,`success`),window.location.search=`?partition=${encodeURIComponent(n)}`}finally{ue=!1}}var O=e(),Pe=`guardian.refreshIntervalMs`;try{let e=localStorage.getItem(Pe);if(e!==null){let t=Number(e);Number.isFinite(t)&&t>=1e3&&t<=12e4&&(O.refreshIntervalMs=t)}}catch{}var Fe=1e3,Ie=12e4;function k(){return O.refreshIntervalMs}function Le(){return Math.max(2e3,Math.floor(k()/5))}function Re(){return Math.min(Ie,k()*3)}function ze(){return Math.min(Ie,k()*3)}var Be=6e4,Ve=6e4;document.addEventListener(`DOMContentLoaded`,()=>{Ye(),Cn(),He().catch($)});async function He(){Je(O.activePanel),await j(),Ue(),O.activePanel===`rolloutsPanel`&&O.selectedPartition&&A()}function Ue(){O.refreshTimer!==void 0&&window.clearTimeout(O.refreshTimer),O.refreshTimer=window.setTimeout(async()=>{try{await j(O.activePanel!==`historyPanel`&&O.activePanel!==`rolloutsPanel`)}catch{}finally{Ue()}},We())}function We(){return document.hidden?Re():Date.now()<O.fastRefreshUntil||Ge()?Le():k()}function Ge(){let e=O.detail;if(!e)return!1;if(String(e?.health?.status??``).toLowerCase()===`pending`)return!0;let t=Array.isArray(e?.intents)?e.intents:[];for(let e of t)switch(String(e?.status??``)){case`Checking`:case`Diffing`:case`Applying`:case`Destroying`:case`Ready`:case`Blocked`:return!0;default:break}return(Array.isArray(e?.health?.services)?e.health.services:[]).some(e=>e?.taskActive===!0)}function Ke(e=Be){let t=Date.now()+e;t>O.fastRefreshUntil&&(O.fastRefreshUntil=t)}function A(){O.rolloutsRefreshTimer!==void 0&&window.clearTimeout(O.rolloutsRefreshTimer),O.rolloutsRefreshTimer=window.setTimeout(async()=>{try{await P(!0)}catch{}finally{A()}},ze())}function qe(){O.rolloutsRefreshTimer!==void 0&&(window.clearTimeout(O.rolloutsRefreshTimer),O.rolloutsRefreshTimer=void 0)}async function j(e=!0){r(`Refreshing…`),O.overview=await t(`/api/overview`),rt(),it();let n=O.selectedPartition,i=(O.overview?.partitions??[]).map(e=>e.name);!n&&i.length>0?await M(i[0],!1):n&&i.includes(n)&&e?await M(n,!1):i.length||(O.selectedPartition=``,O.detail=null,O.history=null,O.rollouts=null,O.expandedRolloutKeys={},ct(),L(),z(),H(),N()),r(`Updated just now`)}async function M(e,n=!0){if(!e)return;Ke();let r=O.selectedPartition===e;O.selectedPartition=e,O.activityDrawer={intentName:``,data:null,loading:!1,error:``},r||(O.expandedAssetKey=``,O.expandedRolloutKeys={},O.diagnosticDetails={},O.history=null,O.historyLoading=!1,O.historyError=``,O.rollouts=null,O.rolloutsLoading=!1,O.rolloutsError=``),N(),it(),O.detail=await t(`/api/partitions/${encodeURIComponent(e)}`),ct(),L(),z(),H(),U(),O.activePanel===`historyPanel`&&nt().catch($),O.activePanel===`rolloutsPanel`&&(P(r).catch($),A())}function Je(e){O.activePanel=e,N(),document.querySelectorAll(`.panel`).forEach(t=>{let n=t.id===e;t.classList.toggle(`active`,n),t.classList.toggle(`hidden`,!n)}),document.querySelectorAll(`[data-tab-target]`).forEach(t=>{t.classList.toggle(`active`,t.dataset.tabTarget===e)}),it(),ct(),L(),z(),fe(),U(),e===`historyPanel`&&O.selectedPartition&&nt().catch($),e===`rolloutsPanel`&&O.selectedPartition&&(P(!0).catch($),A()),e!==`rolloutsPanel`&&qe()}function Ye(){let e=new URLSearchParams(window.location.search),t=e.get(`partition`);t&&(O.selectedPartition=t.trim());let n=e.get(`panel`);[`overviewPanel`,`topologyPanel`,`rolloutsPanel`,`historyPanel`,`scanPanel`].includes(n??``)&&(O.activePanel=n);let r=Number.parseInt(e.get(`historyLimit`)??``,10);Number.isFinite(r)&&r>0&&(O.historyOptions.limit=r);let i=e.get(`historySince`);i&&(O.historyOptions.since=i);let a=e.get(`historyUntil`);a&&(O.historyOptions.until=a),Qe()}function N(){let e=new URLSearchParams(window.location.search);O.selectedPartition?e.set(`partition`,O.selectedPartition):e.delete(`partition`),O.activePanel&&O.activePanel!==`overviewPanel`?e.set(`panel`,O.activePanel):e.delete(`panel`),O.historyOptions.limit===10?e.delete(`historyLimit`):e.set(`historyLimit`,String(O.historyOptions.limit)),O.historyOptions.since?e.set(`historySince`,O.historyOptions.since):e.delete(`historySince`),O.historyOptions.until?e.set(`historyUntil`,O.historyOptions.until):e.delete(`historyUntil`);let t=e.toString();window.history.replaceState(null,``,`${window.location.pathname}${t?`?${t}`:``}`)}async function Xe(e){let n=new URLSearchParams;return n.set(`limit`,String(O.historyOptions.limit)),O.historyOptions.since&&n.set(`since`,O.historyOptions.since),O.historyOptions.until&&n.set(`until`,O.historyOptions.until),t(`/api/partitions/${encodeURIComponent(e)}/history?${n.toString()}`)}async function Ze(e){return t(`/api/partitions/${encodeURIComponent(e)}/rollouts`)}function Qe(){let e=document.getElementById(`historyLimit`);e&&(e.value=String(O.historyOptions.limit));let t=document.getElementById(`historySince`);t&&(t.value=l(O.historyOptions.since));let n=document.getElementById(`historyUntil`);n&&(n.value=l(O.historyOptions.until))}function $e(){let e=document.getElementById(`historyLimit`),t=Number.parseInt(e?.value??``,10);O.historyOptions.limit=Number.isFinite(t)&&t>0?t:10;let n=document.getElementById(`historySince`);O.historyOptions.since=u(n?.value??``);let r=document.getElementById(`historyUntil`);O.historyOptions.until=u(r?.value??``)}async function et(){if($e(),N(),!O.selectedPartition){L();return}await nt(!0)}async function tt(){O.historyOptions={limit:10,since:``,until:``},Qe(),await et()}async function nt(e=!1){if(!O.selectedPartition){O.history=null,O.historyLoading=!1,O.historyError=``,L(),U();return}if(!O.historyLoading){if(!e&&O.history){L(),U();return}O.historyLoading=!0,O.historyError=``,L(),U();try{O.history=await Xe(O.selectedPartition)}catch(e){throw O.history=null,O.historyError=e?.message??`Failed to load history.`,e}finally{O.historyLoading=!1,L(),U()}}}async function P(e=!1){if(!O.selectedPartition){O.rollouts=null,O.rolloutsLoading=!1,O.rolloutsError=``,z(),U();return}if(!O.rolloutsLoading){if(!e&&O.rollouts){z(),U();return}O.rolloutsLoading=!0,O.rolloutsError=``,z(),U();try{O.rollouts=await Ze(O.selectedPartition)}catch(e){throw O.rollouts=null,O.rolloutsError=e?.message??`Failed to load rollouts.`,e}finally{O.rolloutsLoading=!1,z(),U()}}}function rt(){let e=O.overview?.summary??{};n(`summaryPartitions`,e.partitions??0),n(`summaryIntents`,e.intents??0),n(`summaryAssets`,e.assets??0),n(`summaryStable`,e.healthyAssets??e.servicesHealthy??0),n(`summaryAttention`,e.attentionAssets??e.servicesAttention??0),n(`summaryFailed`,e.failingAssets??e.failedIntents??0)}function it(){let e=O.activePanel===`overviewPanel`&&!O.selectedPartition;ot(),e?st():at()}function at(){let e=document.getElementById(`appGrid`);e&&(e.className=`grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5`,e.innerHTML=``)}function ot(){let e=document.getElementById(`partitionList`);if(!e)return;if(!O.overview){e.className=`grid gap-1 loading-state text-sm text-[#566778]`,e.textContent=`Loading partitions…`;return}let t=document.getElementById(`partitionSearch`)?.value.trim().toLowerCase()??``,n=(O.overview?.partitions??[]).filter(e=>t?`${e.name} ${Object.keys(e.labels??{}).join(` `)} ${Object.values(e.labels??{}).join(` `)}`.toLowerCase().includes(t):!0);if(!n.length){e.className=`grid gap-1 empty-state text-sm text-[#566778]`,e.textContent=`No partitions available.`;return}e.className=`grid gap-1`,e.innerHTML=n.map(e=>{let t=e.name===O.selectedPartition,n=Yt([e.errors?.join(`
`),e.lastDisplayStatus?`Last known status: ${e.lastDisplayStatus}`:``]);return`
      <button class="partition-list-item ${t?`active`:``}" data-partition="${a(e.name)}">
        <div class="partition-list-title">
          <strong>${i(e.name)}</strong>
          ${W(e.health,e.displayStatus,`${e.name} status`,n,`partition:${e.name}`)}
        </div>
        <div class="partition-list-meta">
          <span>${e.intentCount??0} intents</span>
          <span>${e.assetCount??0} assets</span>
          <span>${e.healthyAssets??e.servicesHealthy??0} stable</span>
        </div>
      </button>
    `}).join(``),e.querySelectorAll(`[data-partition]`).forEach(e=>{e.addEventListener(`click`,()=>M(e.dataset.partition).catch($))})}function st(){let e=document.getElementById(`appGrid`);if(!e)return;let t=(document.getElementById(`appGridSearch`)?.value??``).trim().toLowerCase(),n=(O.overview?.partitions??[]).filter(e=>t?`${e.name} ${Object.values(e.labels??{}).join(` `)}`.toLowerCase().includes(t):!0);if(!n.length){e.className=`grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5 empty-state text-sm text-[#566778]`,e.textContent=O.overview?`No partitions match the filter.`:`Loading partitions…`;return}e.className=`grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5`,e.innerHTML=n.map(e=>{let t=e.name===O.selectedPartition,n=en(e.labels??{});return`
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
    `}).join(``),e.querySelectorAll(`[data-partition]`).forEach(e=>{e.addEventListener(`click`,()=>M(e.dataset.partition).catch($))})}function ct(){let e=O.detail,t=document.getElementById(`heroContent`);if(!t)return;if(O.activePanel!==`overviewPanel`){U();return}if(!e){t.className=`loading-state text-sm text-[#566778]`,t.textContent=`Select a partition to inspect its current shape.`,[`intentCards`,`attentionAssetsList`,`serviceHealthCards`,`recentEventsList`].forEach(e=>{let t=document.getElementById(e);t&&(t.className=`loading-state text-sm text-[#566778]`,t.textContent=`Choose a partition.`)}),U();return}let n=e.health??{},r={...e.partition?.manifest?.metadata?.labels??{},...e.partition?.manifest?.spec?.labels??{}};t.className=``,t.innerHTML=`
    <div class="hero-grid">
      <div class="hero-main">
        <div class="pill-row mb-2">
          ${W(n.status,n.displayStatus)}
          ${r.role?`<span class="pill">${i(r.role)}</span>`:``}
          ${r.component?`<span class="pill">${i(r.component)}</span>`:``}
          ${r.stack?`<span class="pill">${i(r.stack)}</span>`:``}
          <span class="pill">${i(e.partition.manifest.spec?.deletionPolicy??`orphan`)} deletion</span>
          <span class="pill">${i(e.partition.manifest.spec?.reconciliation?.mode??`manual`)} reconcile</span>
          ${e.compilerError?`<span class="badge badge-failing">Compiler warning</span>`:``}
        </div>
        <h2>${i(e.partition.manifest.metadata.name)}</h2>
        <p>${i(n.summary??`Partition summary unavailable.`)}</p>
        ${n.status===`pending`?$t(e):``}
        <div class="pill-row mt-2">
          ${r.endpoint?`<span class="pill">${i(r.endpoint)}</span>`:``}
          ${r.topology?`<span class="pill">${i(r.topology)}</span>`:``}
          ${r.managedBy?`<span class="pill">${i(r.managedBy)}</span>`:``}
          ${(e.partition.state?.errors??[]).map(e=>`<span class="pill">${i(e)}</span>`).join(``)}
          ${e.compilerError?`<span class="pill">${i(e.compilerError)}</span>`:``}
        </div>
      </div>
      ${lt(`Healthy`,n.healthy??0)}
      ${lt(`Attention`,(n.attention??0)+(n.pending??0))}
      ${lt(`Failing`,n.failing??0)}
    </div>
  `,ut(),F(),bt(),Ct(),U()}function lt(e,t){return`
    <div class="stat-card rounded-lg border border-white/[0.09]">
      <div class="stat-label">${i(e)}</div>
      <div class="stat-value">${t}</div>
    </div>
  `}function ut(){let e=document.getElementById(`attentionAssetsList`);if(!e)return;let t=dt(),n=ft();if(!t.length&&!n.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No assets need attention right now.`;return}e.className=`attention-asset-list`,e.innerHTML=t.map(({intent:e,asset:t})=>{let n=pt(t);return`
    <article class="attention-asset-card attention-asset-card-${a(t.health)}">
      <div class="attention-asset-card-header">
        <div>
          <h3>${i(t.name)}</h3>
          <div class="muted">${i(e.name)} · ${i(Q(t.type))}</div>
        </div>
        ${W(t.health,t.displayStatus,`${e.name} / ${t.name}`,t.summary,`asset:${O.selectedPartition}:${e.name}:${t.name}`)}
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
      ${n.map(({intent:e,asset:t})=>{let n=pt(t);return`
        <div class="progressing-asset-item">
          <div>
            <div>${i(t.name)}</div>
            <div class="muted">${i(e.name)} · ${i(Q(t.type))}${n?` · ${i(n)}`:``}</div>
          </div>
          ${W(t.health,t.displayStatus,`${e.name} / ${t.name}`,t.summary,`asset:${O.selectedPartition}:${e.name}:${t.name}`)}
        </div>
      `}).join(``)}
    </div>
  `:``)}function dt(){return(O.detail?.intents??[]).flatMap(e=>(e.assets??[]).map(t=>({intent:e,asset:t}))).filter(({asset:e})=>e?.health===`failing`||e?.health===`attention`||e?.health===`drifted`||e?.health===`drifted-locked`).sort((e,t)=>{let n=mt(e.asset.health)-mt(t.asset.health);return n===0?e.intent.name===t.intent.name?e.asset.name.localeCompare(t.asset.name):e.intent.name.localeCompare(t.intent.name):n})}function ft(){return(O.detail?.intents??[]).flatMap(e=>(e.assets??[]).map(t=>({intent:e,asset:t}))).filter(({asset:e})=>e?.health===`pending`).sort((e,t)=>e.intent.name===t.intent.name?e.asset.name.localeCompare(t.asset.name):e.intent.name.localeCompare(t.intent.name))}function pt(e){let t=String(e?.summary??``).trim(),n=String(e?.observedHealth?.summary??``).trim(),r=String(e?.status??``);return(r===`Drifted`||r===`DriftedLocked`)&&n?t.includes(n)?t:t?`${t}: ${n}`:n:t}function mt(e){return e===`failing`?0:e===`drifted-locked`?1:e===`drifted`?2:e===`attention`?3:4}function F(){let e=document.getElementById(`intentCards`);if(!e)return;let t=O.detail?.intents??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No intents defined for this partition yet.`;return}e.className=`intent-stack`,e.innerHTML=t.map(e=>{let t=xn(e.assets??[]),n=O.activityDrawer.intentName===e.name,r=Sn(e.assets??[]).map(t=>`
      <section class="intent-asset-group">
        <div class="intent-asset-group-title">
          <span class="intent-lane-group-dot" style="background:${bn(t.category)}"></span>
          <span>${i(t.category)} · ${t.assets.length}</span>
        </div>
        <div class="asset-grid">
          ${t.assets.map(t=>{let n=pt(t),r=ht(e.name,t.name),o=O.expandedAssetKey===r,s=_n(t.type),c=`asset-detail-${I(r)}`,l=[...t.quickFacts??[]].sort((e,t)=>e.label===`Release`?-1:+(t.label===`Release`)).map(e=>`<span class="fact${un(e.label)}" title="${a(dn(e.label))}">${i(e.label)}: ${i(e.value)}</span>`).join(``),u=o?hn(t,{limit:2**53-1,truncateAt:160}):``,d=o?gn(t.outputs??{},[],{limit:2**53-1,truncateAt:160}):``,f=o?(t.references??[]).map(e=>`<span class="fact">${i(e)}</span>`).join(``):``,p=o&&(t.dependsOn??[]).length?(t.dependsOn??[]).map(e=>`<span class="fact">${i(e)}</span>`).join(``):``;return`
              <article
                class="asset-chip asset-chip-${a(t.health??`neutral`)}${o?` asset-chip-expanded`:``}"
                data-asset-toggle="${a(r)}"
                data-asset-card="${a(I(r))}"
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
                  ${W(t.health,t.displayStatus,`${e.name} / ${t.name}`,t.summary,`asset:${O.selectedPartition}:${e.name}:${t.name}`)}
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
              ${W(e.health,e.displayStatus,`${e.name} intent`,e.summary,`intent:${O.selectedPartition}:${e.name}`)}
              ${(()=>{let t=Kt(e);return W(t.status,t.label,`${e.name} snapshot freshness`,t.detail,`intent-freshness:${O.selectedPartition}:${e.name}`)})()}
              ${e.lastDeployment?.createdAt?(()=>{let t=new Date(e.lastDeployment.createdAt).getTime(),n=Number.isFinite(t)?Date.now()-t:null,r=n!==null&&n>=0?`Applied ${q(n)}`:`Applied ${c(e.lastDeployment.createdAt)}`;return`<span class="pill" title="${a(`Last deployment: ${c(e.lastDeployment.createdAt)}`)}">${i(r)}</span>`})():``}
              <span class="pill">${i(e.targetSummary??`Unassigned`)}</span>
              ${(e.joined??[]).map(e=>`<span class="pill">joins ${i(e)}</span>`).join(``)}
              ${t.map(e=>`<span class="pill">${i(`${e.category} ${e.count}`)}</span>`).join(``)}
              ${e.locked?`<span class="pill">locked</span>`:``}
              <button class="activity-btn ${n?`active`:``}" type="button" data-activity-intent="${a(e.name)}">&#9685;</button>
            </div>
          </div>
        </div>
        ${n?yt():``}
        ${r}
      </article>
    `}).join(``),e.querySelectorAll(`[data-activity-intent]`).forEach(e=>{e.addEventListener(`click`,()=>vt(e.dataset.activityIntent).catch($))}),e.querySelectorAll(`[data-asset-toggle]`).forEach(e=>{e.addEventListener(`click`,()=>gt(e.dataset.assetToggle??``)),e.addEventListener(`keydown`,t=>{t.key!==`Enter`&&t.key!==` `||(t.preventDefault(),gt(e.dataset.assetToggle??``))})})}function ht(e,t){return`${e}::${t}`}function I(e){return e.replace(/[^a-zA-Z0-9_-]+/g,`-`)}function gt(e){e&&(O.expandedAssetKey=O.expandedAssetKey===e?``:e,F())}function _t(e){if(!e)return;O.expandedAssetKey=e,F();let t=I(e);requestAnimationFrame(()=>{document.querySelector(`[data-asset-card="${t}"]`)?.scrollIntoView({behavior:`smooth`,block:`center`,inline:`nearest`})})}async function vt(e){if(O.activityDrawer.intentName===e){O.activityDrawer={intentName:``,data:null,loading:!1,error:``},F();return}O.activityDrawer={intentName:e,data:null,loading:!0,error:``},F();try{let n=O.selectedPartition;O.activityDrawer={intentName:e,data:await t(`/api/partitions/${encodeURIComponent(n)}/intents/${encodeURIComponent(e)}/activity`),loading:!1,error:``}}catch(t){O.activityDrawer={intentName:e,data:null,loading:!1,error:t.message??`Failed to load activity`}}F()}function yt(){let{data:e,loading:t,error:n}=O.activityDrawer;if(t)return`<div class="activity-drawer"><div class="activity-loading">Loading activity…</div></div>`;if(n)return`<div class="activity-drawer"><div class="activity-error">${i(n)}</div></div>`;if(!e)return`<div class="activity-drawer"><div class="activity-loading">No activity data.</div></div>`;let r=e.timestamps??{},o=[{label:`Queued`,value:r.lastQueuedAt},{label:`Check`,value:r.lastCheckAt},{label:`Diff`,value:r.lastDiffAt},{label:`Apply`,value:r.lastApplyAt}].filter(e=>e.value&&e.value!==`0001-01-01T00:00:00Z`),s=e.logs??[],l=e.drift,u=r.lastQueuedAt?Date.now()-new Date(r.lastQueuedAt).getTime():null,d=u!==null&&u>0?q(u):null,f=``;return e.taskTimedOut?f=`
      <div class="activity-task-status activity-task-stale">
        <span class="activity-task-status-icon">⚠</span>
        <div>
          <strong>Task appears stuck</strong>
          <div class="activity-task-status-detail">Queued ${d??`a while`} ago. The worker may be down or the task lost. Check pusher logs for errors.</div>
        </div>
      </div>`:e.taskActive&&(f=`
      <div class="activity-task-status activity-task-running">
        <span class="activity-task-status-icon">◌</span>
        <div>
          <strong>Task in progress</strong>
          <div class="activity-task-status-detail">Running for ${d??`unknown duration`}. ${e.lastOp?`Current op: ${i(e.lastOp)}`:``}</div>
        </div>
      </div>`),`
    <div class="activity-drawer">
      <div class="activity-header">
        <span class="activity-header-title">Activity log</span>
        ${e.lastOp?`<span class="activity-op-badge">last op: ${i(e.lastOp)}</span>`:``}
        ${e.lastTaskID?`<span class="activity-task-id">${i(e.lastTaskID.slice(0,16))}…</span>`:``}
      </div>
      ${f}
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
  `}function bt(){let e=document.getElementById(`serviceHealthCards`);if(!e)return;let t=O.detail?.health?.services??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No service-like assets to score yet.`;return}let n=t.filter(e=>e.status===`healthy`).length,r=t.filter(e=>e.status===`attention`).length,o=t.filter(e=>e.status===`failing`).length,s=t.filter(e=>e.taskActive).length,c=t.filter(e=>e.taskTimedOut).length;e.className=`service-stack`,e.innerHTML=`
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
        ${W(e.status,e.displayStatus,`${e.intent} / ${e.asset}`,e.summary,`service:${O.selectedPartition}:${e.intent}:${e.asset}`)}
      </div>
      <p class="service-card-note">${i(xt(e))}</p>
      <div class="service-health-meta">
        ${St(e)}
      </div>
      <div class="service-card-actions">
        <button class="btn-secondary service-card-action" type="button" data-service-focus="${a(ht(e.intent,e.asset))}">Open details</button>
      </div>
    </article>
  `).join(``)}
  `,e.querySelectorAll(`[data-service-focus]`).forEach(e=>{e.addEventListener(`click`,()=>_t(e.dataset.serviceFocus??``))})}function xt(e){if(e.taskTimedOut)return`Last reconcile task timed out. Open details in the intent map for config and outputs.`;if(e.taskActive)return`Reconcile is currently running for this service.`;switch(e.status){case`healthy`:return`Operational summary only. Configuration and ports stay in the intent map.`;case`pending`:return`Waiting for the first successful reconcile.`;case`attention`:return String(e.summary??`Needs attention.`);case`failing`:return String(e.summary??`Service is failing.`);default:return String(e.summary??`Service status unavailable.`)}}function St(e){let t=[];e.taskActive&&t.push(`reconcile running`),e.taskTimedOut&&t.push(`last task timed out`);let n=c(e.lastUpdatedAt);return n!==`—`&&t.push(`updated ${n}`),t.map(e=>`<span class="service-health-meta-item">${i(e)}</span>`).join(``)}function Ct(){let e=document.getElementById(`recentEventsList`);if(!e)return;let t=O.detail?.recentEvents??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Recent event history loads only in the History tab.`;return}e.className=`timeline-stack`,e.innerHTML=Bt(t).map(Vt).join(``)}function L(){let e=document.getElementById(`deploymentTimeline`),t=document.getElementById(`eventTimeline`);if(!e||!t)return;if(!O.selectedPartition){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a partition to inspect deployment history.`,t.className=`empty-state text-sm text-[#566778]`,t.textContent=`Select a partition to inspect event history.`;return}if(O.historyLoading){e.className=`loading-state text-sm text-[#566778]`,e.textContent=`Loading deployment history…`,t.className=`loading-state text-sm text-[#566778]`,t.textContent=`Loading event history…`;return}if(O.historyError){e.className=`empty-state text-sm text-[#566778]`,e.textContent=O.historyError,t.className=`empty-state text-sm text-[#566778]`,t.textContent=O.historyError;return}let n=O.history;if(!n){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Open the History tab to load deployment history.`,t.className=`empty-state text-sm text-[#566778]`,t.textContent=`Open the History tab to load event history.`;return}let r=(document.getElementById(`historyFilter`)?.value??``).trim().toLowerCase(),i=(n.deployments??[]).filter(e=>r?`${e.intent} ${e.deploymentRevision} ${(e.assets??[]).map(e=>`${e.asset} ${e.version??``}`).join(` `)}`.toLowerCase().includes(r):!0),a=(n.events??[]).filter(e=>r?`${e.intent??``} ${e.title??``} ${e.message??``}`.toLowerCase().includes(r):!0);e.className=i.length?`timeline-stack`:`empty-state text-sm text-[#566778]`,e.innerHTML=i.length?i.map(Ht).join(``):`No deployment entries match the current filter.`;let o=document.getElementById(`historyGroupToggle`),s=!o||o.checked?Bt(a):a;t.className=s.length?`timeline-stack`:`empty-state text-sm text-[#566778]`,t.innerHTML=s.length?s.map(Vt).join(``):`No events match the current filter.`}function wt(e){let t=e.map(e=>new Date(e.createdAt).getTime()).filter(e=>Number.isFinite(e)),n=e.map(e=>G(e.appliedAt)).filter(e=>e!==null),r=t.length?Math.min(...t):Date.now(),i=Math.max(...t,...n,Date.now()),a=i+Math.max(6e4,Math.floor((i-r||6e4)*.08));return{minTime:r,paddedMaxTime:a,span:Math.max(1,a-r)}}function R(e,t,n){return Math.min(100,Math.max(0,(e-t)/n*100))}function z(){let e=document.getElementById(`rolloutsTimeline`);if(!e)return;if(!O.selectedPartition){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a partition to inspect rollout history.`;return}if(O.rolloutsLoading){e.className=`loading-state text-sm text-[#566778]`,e.textContent=`Loading rollouts…`;return}if(O.rolloutsError){e.className=`empty-state text-sm text-[#566778]`,e.textContent=O.rolloutsError;return}let t=O.rollouts?.rollouts??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No archived rollouts were found for this partition yet.`;return}let{minTime:n,paddedMaxTime:r,span:a}=wt(t),o=new Map;for(let e of t){let t=String(e.intent??``).trim()||`(unknown intent)`,n=o.get(t);n?n.push(e):o.set(t,[e])}let s=Array.from(o.entries()).map(([e,t])=>({intent:e,items:t.slice().sort((e,t)=>new Date(e.createdAt).getTime()-new Date(t.createdAt).getTime())})).sort((e,t)=>{let n=new Date(e.items[e.items.length-1]?.createdAt??0).getTime();return new Date(t.items[t.items.length-1]?.createdAt??0).getTime()-n}),c=R(Date.now(),n,a),l=Ot(n,r,6),u=t.slice().sort((e,t)=>new Date(t.createdAt).getTime()-new Date(e.createdAt).getTime());e.className=`rollout-view`,e.innerHTML=`
    <div class="rollout-timeline-wrap">
      <!-- Sticky shared axis header -->
      <div class="rollout-axis-header">
        <div class="rollout-axis-label-col"></div>
        <div class="rollout-axis-track-col">
          <div class="rollout-now-line" style="left:${c}%" aria-hidden="true">
            <span class="rollout-now-label">NOW</span>
          </div>
          <div class="rollout-axis-ticks" role="presentation">
            ${l.map(e=>`
              <div class="rollout-axis-tick" style="left:${e.left}%">
                <span>${i(e.label)}</span>
              </div>
            `).join(``)}
          </div>
        </div>
      </div>

      <!-- Per-intent lanes -->
      <div class="rollout-lanes-body">
        ${s.map(e=>Et(e,n,a)).join(``)}
      </div>
    </div>

    <!-- Event stream -->
    <div class="rollout-event-stream">
      <div class="rollout-event-stream-header">
        <span class="rollout-event-stream-title">Event stream</span>
        <span class="rollout-event-stream-count">${u.length} event${u.length===1?``:`s`}</span>
      </div>
      <div class="rollout-event-list">
        ${u.map(e=>Tt(e)).join(``)}
      </div>
    </div>
  `,e.querySelectorAll(`[data-rollout-toggle]`).forEach(e=>{e.addEventListener(`click`,t=>{t.preventDefault();let n=e.dataset.rolloutToggle??``;n&&Lt(n)})})}function Tt(e){let t=B(e),n=V(e),r=!!O.expandedRolloutKeys[n],s=(e.deploymentRevision??``).slice(0,8),l=e.assets??[],u=t.statusText,d=t.pointClass;e.newIntent?(u=`New intent`,d=`point-new`):e.rollback?(u=`Rollback`,d=`point-rollback`):e.selfHealing?(u=`Self-heal`,d=`point-heal`):e.current?e.healthStatus===`failing`?(u=`Error`,d=`point-failing`):e.healthStatus===`attention`?(u=`Degraded`,d=`point-degraded`):(u=`Deployed`,d=`point-current`):u=`Deployed`;let f=l.filter(e=>e.change&&e.change!==`unchanged`),p=f.length?f.slice(0,3).map(e=>i(e.name)).join(`, `)+(f.length>3?` +${f.length-3}`:``):l.length?`${l.length} asset${l.length===1?``:`s`}`:``;return`
    <div class="rollout-event-row${e.current?` event-row-current`:``}${r?` event-row-expanded`:``}">
      <div class="rollout-event-row-main">
        <div class="rollout-event-dot-col">
          <span class="rollout-event-dot rollout-lane-point ${d}" aria-hidden="true"></span>
          <span class="rollout-event-line" aria-hidden="true"></span>
        </div>
        <div class="rollout-event-body">
          <div class="rollout-event-top">
            <span class="rollout-event-intent">${i(e.intent??`—`)}</span>
            <span class="rollout-event-type rollout-type-${d.replace(`point-`,``)}">${i(u)}</span>
            ${e.current?`<span class="rollout-event-current-badge">current</span>`:``}
          </div>
          <div class="rollout-event-meta">
            <span class="rollout-event-time" title="${a(c(e.createdAt))}">${i(jt(new Date(e.createdAt).getTime()))}</span>
            <span class="rollout-event-rev" title="${a(e.deploymentRevision??``)}">${i(s)}</span>
            ${e.target?`<span class="rollout-event-target">${i(e.target)}</span>`:``}
            ${p?`<span class="rollout-event-assets">${p}</span>`:``}
          </div>
          ${e.summary?`<div class="rollout-event-summary">${i(e.summary)}</div>`:``}
          ${e.rollback&&e.rollbackTo?`<div class="rollout-event-rollback-note">↩ rolled back to <code>${i(e.rollbackTo.slice(0,8))}</code>${e.rollbackReason?` — ${i(e.rollbackReason)}`:``}</div>`:``}
          ${e.healthSummary&&e.current?`<div class="rollout-event-health-note">${i(e.healthSummary)}</div>`:``}
        </div>
        <button
          class="rollout-event-expand-btn"
          type="button"
          data-rollout-toggle="${a(n)}"
          aria-expanded="${r?`true`:`false`}"
          title="${r?`Hide assets`:`Show ${l.length} asset${l.length===1?``:`s`}`}"
        >${r?`▲`:`▼`}</button>
      </div>
      ${r&&l.length?`
        <div class="rollout-event-assets-detail">
          ${l.map(e=>`
            <div class="rollout-event-asset-row">
              <span class="rollout-event-asset-type">${i(e.type||`Asset`)}</span>
              <span class="rollout-event-asset-name">${i(e.name)}</span>
              <span class="rollout-event-asset-version">${i(e.version?e.version.slice(0,8):s)}</span>
              ${W(zt(e.change),o(e.change||`updated`),void 0,void 0,void 0,It(e.change))}
            </div>
          `).join(``)}
        </div>
      `:``}
    </div>
  `}function Et(e,t,n){let r=Date.now(),o=e.items.find(e=>e.current)??e.items[e.items.length-1],s=o?B(o):null,l=[];for(let i=0;i<e.items.length;i++){let a=e.items[i],o=i===e.items.length-1,s=new Date(a.createdAt).getTime(),c=K(a.appliedAt),u=o?Math.max(s,r):new Date(e.items[i+1].createdAt).getTime();if(o&&c!==null&&c>s&&c<u){let e=R(s,t,n),r=Math.max(e+.5,R(c,t,n)),i=Math.max(.5,r-e);l.push(`<div class="rollout-lane-segment seg-deploying" style="left:${e.toFixed(3)}%;width:${i.toFixed(3)}%" title="Deploying: ${jt(s)}"></div>`);let o=Math.max(r+.5,R(u,t,n)),d=Math.max(.5,o-r),f=B(a).segmentClass;l.push(`<div class="rollout-lane-segment ${f}" style="left:${r.toFixed(3)}%;width:${d.toFixed(3)}%"></div>`)}else{let r=R(s,t,n),c=Math.max(r+1,R(u,t,n)),d=Math.max(1,c-r),f=B(o?a:e.items[i+1]);l.push(`<div class="rollout-lane-segment ${f.segmentClass}" style="left:${r.toFixed(3)}%;width:${d.toFixed(3)}%"></div>`)}}let u=e.items.map((r,o)=>{let s=V(r),l=!!O.expandedRolloutKeys[s],u=new Date(r.createdAt).getTime(),d=Math.max(.5,Math.min(99.5,R(u,t,n))),f=B(r),p=(r.deploymentRevision??``).slice(0,7);return`
      <div class="rollout-point-group" style="left:${d.toFixed(3)}%">
        <button
          class="rollout-lane-point ${f.pointClass}${l?` active`:``}"
          type="button"
          data-rollout-toggle="${a(s)}"
          aria-expanded="${l?`true`:`false`}"
          title="${a(`${f.statusText}: ${r.deploymentRevision} — ${c(r.createdAt)}`)}"
        ></button>
        <span class="rollout-point-label" title="${a(r.deploymentRevision??``)}">${i(p)}</span>
        ${r.current&&o===e.items.length-1?`<span class="rollout-point-current-badge">now</span>`:``}
      </div>
    `}),d=Pt(Mt(e.items)),f=s?`<span class="rollout-lane-status-chip chip-${s.pointClass.replace(`point-`,``)}" title="${a(s.statusText)}">${i(s.statusText)}</span>`:``;return`
    <div class="rollout-lane-row">
      <div class="rollout-lane-label-col">
        <div class="rollout-lane-intent" title="${a(e.intent)}">${i(e.intent)}</div>
        <div class="rollout-lane-label-meta">
          ${f}
          <span class="rollout-lane-count">${e.items.length}×</span>
        </div>
      </div>
      <div class="rollout-lane-chart-col">
        <div class="rollout-lane-track-wrap">
          <div class="rollout-lane-track"></div>
          <div class="rollout-lane-segments">${l.join(``)}</div>
          <div class="rollout-lane-points">${u.join(``)}</div>
        </div>
        ${d}
        ${e.items.filter(e=>O.expandedRolloutKeys[V(e)]).map(e=>Dt(e)).map(e=>`<div class="rollout-lane-card-wrap">${e}</div>`).join(``)}
      </div>
    </div>
  `}function Dt(e){let t=B(e),n=e.assets??[],r=n.length,s=V(e),l=!!O.expandedRolloutKeys[s],u=e.healthText||t.statusText,d=e.healthSummary||``,f=W(t.statusKey,t.statusText,u,d,void 0,Ft(t.statusText)),p=(e.deploymentRevision??``).slice(0,8),m=e.deploymentRevision??``;return`
    <div class="rollout-detail-card ${t.cardClass}">
      <div class="rollout-detail-header">
        <div class="rollout-card-top">
          <div class="rollout-card-rev" title="${a(m)}">
            <span class="rollout-card-rev-hash">${i(p)}</span>
          </div>
          <div class="rollout-card-status-row">
            ${f}
            ${e.rollbackTo?`<span class="rollout-card-rollback-info" title="Rolled back to this revision">rolled back to <code>${i(e.rollbackTo.slice(0,8))}</code></span>`:``}
          </div>
        </div>

        <div class="rollout-card-meta">
          <span title="${a(c(e.createdAt))}">${i(jt(new Date(e.createdAt).getTime()))}</span>
          ${e.target?`<span>${i(e.target)}</span>`:``}
          ${e.summary?`<span class="rollout-card-summary-text">${i(e.summary)}</span>`:``}
        </div>

        ${Rt(e)}
      </div>

      ${l&&r?`
        <div class="rollout-card-assets">
          ${n.map(t=>`
            <div class="rollout-card-asset">
              <div class="rollout-card-asset-name">${i(t.name)}</div>
              <div class="rollout-card-asset-meta">
                <span>${i(t.type||`Asset`)}</span>
                <span class="rollout-card-asset-version">${i(t.version||e.deploymentRevision)}</span>
                ${W(zt(t.change),o(t.change||`updated`),void 0,void 0,void 0,It(t.change))}
              </div>
            </div>
          `).join(``)}
        </div>
      `:``}

      <button
        class="rollout-card-toggle"
        type="button"
        data-rollout-toggle="${a(s)}"
        aria-expanded="${l?`true`:`false`}"
      >
        <span>${l?`Hide details`:r?`Show ${r} asset${r===1?``:`s`}`:`No assets`}</span>
      </button>
    </div>
  `}function Ot(e,t,n){if(n<=1||t<=e)return[{left:0,label:kt(e)}];let r=[],i=t-e;for(let t=0;t<n;t++){let a=t/(n-1),o=e+i*a;r.push({left:Number((a*100).toFixed(3)),label:kt(o)})}return r}function kt(e){let t=new Date(e);return Number.isNaN(t.getTime())?`—`:`${t.toLocaleDateString(void 0,{month:`short`,day:`numeric`})}, ${t.toLocaleTimeString(void 0,{hour:`2-digit`,minute:`2-digit`,hour12:!1})}`}function At(e){let t=new Date(e);return Number.isNaN(t.getTime())?`—`:t.toLocaleTimeString(void 0,{hour:`2-digit`,minute:`2-digit`,hour12:!1})}function jt(e){let t=new Date(e);return Number.isNaN(t.getTime())?`—`:t.toLocaleDateString(void 0,{month:`short`,day:`numeric`})+` `+t.toLocaleTimeString(void 0,{hour:`2-digit`,minute:`2-digit`,hour12:!1})}function Mt(e){let t=[],n=Date.now(),r=36e5,i=e.filter(e=>!!e.rollback);for(let a=0;a<e.length;a++){let o=e[a],s=e[a+1],c=K(o.createdAt);if(c===null)continue;let l=K(o.appliedAt),u=s?K(s.createdAt):null,d=s!=null&&!!s.rollback&&s.rollbackTo===o.deploymentRevision;if(o.rollback){let e=l||Math.min(c+6e4,u||c+6e4);e>c&&t.push({type:`recovering`,label:`Recover`,startMs:c,endMs:e,cssClass:`phase-recovering`,rollout:o});let i=u||(o.current?n:c+6e4),a=o.current?i:Math.min(i,e+r);if(a>e){let n=`healthy`,r=`phase-healthy`;o.healthStatus===`failing`?(n=`failing`,r=`phase-failing`):o.healthStatus===`attention`&&(n=`degraded`,r=`phase-degraded`);let i=o.current&&!s&&a-e>300*1e3;t.push({type:n,label:o.healthStatus===`failing`?`Failed`:o.healthStatus===`attention`?`Degraded`:`Healthy`,startMs:e,endMs:a,cssClass:i?r+` phase-compressed`:r,rollout:o})}continue}if(d){let e=u||c+3e4;t.push({type:`failing`,label:`Failed`,startMs:c,endMs:e,cssClass:`phase-failing`,rollout:o});continue}let f=l||Math.min(c+3e4,u||c+3e4),p=i.find(e=>{let t=K(e.createdAt);return t!==null&&t>c&&f!==null&&t<f});if(p&&l!==null){let e=K(p.createdAt),n=2e3;e>c+2e3&&t.push({type:`deploying`,label:`Deploy`,startMs:c,endMs:e,cssClass:`phase-deploying`,rollout:o}),t.push({type:`failing`,label:`Failed`,startMs:e,endMs:e+n,cssClass:`phase-failing`,rollout:o}),t.push({type:`recovering`,label:`Recover`,startMs:e+n,endMs:l,cssClass:`phase-recovering`,rollout:o})}else f>c&&t.push({type:`deploying`,label:`Deploy`,startMs:c,endMs:f,cssClass:`phase-deploying`,rollout:o});let m=u||(o.current?n:c+6e4),h=o.current?m:Math.min(m,f+r);if(h>f){let e=`healthy`,n=`phase-healthy`;o.healthStatus===`failing`?(e=`failing`,n=`phase-failing`):o.healthStatus===`attention`?(e=`degraded`,n=`phase-degraded`):o.selfHealing&&(e=`recovering`,n=`phase-recovering`);let r=o.current&&!s&&h-f>300*1e3;t.push({type:e,label:o.selfHealing?`Recover`:o.healthStatus===`failing`?`Failed`:o.healthStatus===`attention`?`Degraded`:`Healthy`,startMs:f,endMs:h,cssClass:r?n+` phase-compressed`:n,rollout:o})}}return t.sort((e,t)=>e.startMs-t.startMs),t}function Nt(e){if(e<=0)return``;let t=Math.round(e/1e3);if(t<120)return t+`s`;if(t<3600)return Math.round(t/60)+`min`;if(t<86400){let e=Math.floor(t/3600),n=Math.round(t%3600/60);return n>0?e+`h `+n+`min`:e+`h`}let n=Math.floor(t/86400),r=Math.round(t%86400/3600);return n<30?r>0?n+`d `+r+`h`:n+`d`:Math.round(n/30)+`mo`}function Pt(e){if(!e.length)return``;let t=e.map(e=>{let t=(e.endMs-e.startMs)/1e3;return e.type===`healthy`||e.type===`degraded`?Math.min(t,300):Math.max(t,20)}),n=t.reduce((e,t)=>e+t,0);return`
    <div class="rollout-phase-bar">
      ${e.map((r,o)=>{let s=n>0?Math.max(2,t[o]/n*100):100/e.length,c=Nt(r.endMs-r.startMs);return c?`${r.label}${c}`:r.label,`<div class="rollout-phase ${r.cssClass}" style="flex:0 1 ${s}%" title="${a(r.label+`: `+At(r.startMs)+` → `+At(r.endMs)+(c?` (`+c+`)`:``))}"><span>${i(r.label)}</span>${c?`<span class="rollout-phase-duration">${c}</span>`:``}</div>`}).join(``)}
    </div>
  `}function B(e){return e.current?e.rollback?{statusText:`Current (rollback)`,statusKey:`attention`,cardClass:`card-current card-rollback`,segmentClass:`seg-rollback`,pointClass:`point-rollback`}:e.healthStatus===`failing`?{statusText:`Error`,statusKey:`failing`,cardClass:`card-current card-failing`,segmentClass:`seg-failing`,pointClass:`point-failing`}:e.healthStatus===`attention`?{statusText:`Degraded`,statusKey:`attention`,cardClass:`card-current card-degraded`,segmentClass:`seg-degraded`,pointClass:`point-degraded`}:e.healthStatus===`healthy`||e.healthStatus===`ready`?{statusText:`Current`,statusKey:`healthy`,cardClass:`card-current`,segmentClass:`seg-current`,pointClass:`point-current`}:e.newIntent?{statusText:`New intent`,statusKey:`pending`,cardClass:`card-current card-new`,segmentClass:`seg-new`,pointClass:`point-new`}:{statusText:`Current`,statusKey:`healthy`,cardClass:`card-current`,segmentClass:`seg-current`,pointClass:`point-current`}:e.newIntent?{statusText:`New intent`,statusKey:`pending`,cardClass:`card-new`,segmentClass:`seg-new`,pointClass:`point-new`}:e.rollback?{statusText:`Rollback`,statusKey:`attention`,cardClass:`card-rollback`,segmentClass:`seg-rollback`,pointClass:`point-rollback`}:e.selfHealing?{statusText:`Self-heal`,statusKey:`neutral`,cardClass:``,segmentClass:`seg-heal`,pointClass:`point-heal`}:{statusText:`Superseded`,statusKey:`neutral`,cardClass:``,segmentClass:`seg-old`,pointClass:`point-old`}}function Ft(e){switch(e){case`Current`:return`This is the latest successful rollout revision for the intent.`;case`Current (rollback)`:return`The intent was rolled back to this revision after a failed deploy.`;case`Error`:return`The current rollout has failing assets (e.g. ImagePullBackOff, CrashLoopBackOff).`;case`Degraded`:return`The current rollout has assets that are not fully ready.`;case`New intent`:return`The intent appeared for the first time in this rollout.`;case`Self-heal`:return`Guardian reconciled drift and re-applied the desired state without a new intent change.`;case`Rollback`:return`Guardian reverted to a previously known-good deployment revision.`;case`Recover`:return`Guardian is re-deploying after a failed APPLY attempt (auto-heal rollout).`;default:return`An older rollout kept for history; it is no longer the active revision.`}}function It(e){switch((e??``).toLowerCase()){case`added`:return`This asset was introduced in this rollout.`;case`removed`:return`This asset was removed in this rollout.`;case`refreshed`:return`Guardian re-applied this asset during self-heal to restore desired state.`;case`updated`:return`This asset definition or effective version changed in this rollout.`;default:return`This asset participated in this rollout.`}}function Lt(e){e&&(O.expandedRolloutKeys={...O.expandedRolloutKeys,[e]:!O.expandedRolloutKeys[e]},z())}function V(e){return`${e.intent??``}::${e.deploymentRevision??``}`}function Rt(e){if(!e.rollback)return``;let t=[];e.rollbackTo&&t.push(`Rolled back to revision <code>${i(e.rollbackTo)}</code>`);let n=String(e.rollbackReason??``).trim();return n&&t.push(`Reason: ${i(n)}`),t.length?`<div class="rollout-tl-row"><div class="rollout-tl-rollback-detail">${t.join(` &middot; `)}</div></div>`:``}function zt(e){switch((e??``).toLowerCase()){case`added`:return`pending`;case`removed`:return`attention`;default:return`healthy`}}function Bt(e){let t=new Map;for(let n of e){let e=`${n.intent??``}::${n.title}`,r=t.get(e);!r||new Date(n.timestamp)>new Date(r.latest.timestamp)?t.set(e,{latest:n,count:(r?.count??0)+1}):r.count++}return Array.from(t.values()).sort((e,t)=>new Date(t.latest.timestamp).getTime()-new Date(e.latest.timestamp).getTime()).map(e=>({...e.latest,groupCount:e.count}))}function Vt(e){let t=e.groupCount>1?`<span class="event-count-pill" title="${e.groupCount} occurrences">${e.groupCount}×</span>`:``,n=(e.title??``).toLowerCase().replace(/[^a-z0-9]/g,``),r=(e.message??``).toLowerCase().replace(/[^a-z0-9]/g,``),a=e.message&&r!==n;return`
    <article class="timeline-card">
      <div class="timeline-head">
        <div>
          <span class="event-type-eyebrow">Event type</span>
          <h3>${i(e.title??`Event`)} ${t}</h3>
          ${a?`<div class="muted">${i(e.message)}</div>`:``}
        </div>
        ${W(e.status,e.displayStatus,e.title??`Event`,e.message??``)}
      </div>
      <div class="timeline-meta">
        <span>${c(e.timestamp)}</span>
        ${e.intent?`<span>${i(e.intent)}</span>`:``}
        ${e.taskID?`<span>${i(e.taskID)}</span>`:``}
        ${e.deploymentRevision?`<span>${i(e.deploymentRevision)}</span>`:``}
      </div>
    </article>
  `}function Ht(e){return`
    <article class="timeline-card">
      <div class="timeline-head">
        <div>
          <h3>${i(e.intent)}</h3>
          <div class="muted">${i(e.deploymentRevision)}</div>
        </div>
        ${W(e.status,e.displayStatus)}
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
              ${W(e.status,e.displayStatus)}
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
  `}function H(){let e=document.getElementById(`topologyCanvas`);if(!e)return;let t=O.detail?.topology;g(document.getElementById(`topologyLegend`)),se({canvas:e,topology:t,zoom:O.topology.zoom,savedPositions:O.topology.nodePositions,selectedNodeId:O.topology.selectedNodeId,filters:{contains:document.getElementById(`showContainEdges`)?.checked??!0,join:document.getElementById(`showJoinEdges`)?.checked??!0,dependsOn:document.getElementById(`showAssetEdges`)?.checked??!0,outputRef:document.getElementById(`showOutputEdges`)?.checked??!0},onSelectNode:(e,t)=>{O.topology.selectedNodeId=e,O.topology.nodePositions=t,Ut(),H()},onDragNode:(e,t)=>{O.topology.nodePositions=t}}),Ut()}function Ut(){let e=document.getElementById(`topologyDetails`);if(!e)return;let t=O.detail?.topology;if(!t?.nodes?.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a node to inspect its status, metadata, and linked details.`;return}let n=new Map(t.nodes.map(e=>[e.id,e])),r=n.get(O.topology.selectedNodeId);if(!r){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a node to inspect its status, metadata, and linked details.`;return}let a={contains:document.getElementById(`showContainEdges`)?.checked??!0,join:document.getElementById(`showJoinEdges`)?.checked??!0,dependsOn:document.getElementById(`showAssetEdges`)?.checked??!0,outputRef:document.getElementById(`showOutputEdges`)?.checked??!0},s=(t.edges??[]).filter(e=>a[e.kind]!==!1).filter(e=>e.from===r.id||e.to===r.id),c=r.kind===`asset`?nn(r.intent,r.asset??r.label):null,l=r.kind===`intent`?tn(r.intent??r.label):null,u=c?hn(c):``,d=gn(c?.outputs??l?.outputs??{},c?[]:l?.outputHints??[]),f=(c?.references??[]).map(e=>`<span class="fact">${i(e)}</span>`).join(``),p=s.map(e=>{let t=e.from===r.id?e.to:e.from,a=n.get(t);return a?`
      <div class="topology-detail-row">
        <span class="topology-detail-direction">${e.from===r.id?`out`:`in`}</span>
        <span class="topology-detail-name">${i(a.label)}</span>
        <span class="topology-detail-kind">${i(o(e.kind))}</span>
      </div>
    `:``}).join(``);e.className=`topology-detail-card`,e.innerHTML=`
    <div style="--node-accent:${rn(r)}">
      <div class="topology-detail-header">
        <div class="topology-detail-icon">${i(an(r))}</div>
        <div>
          <h3>${i(r.label)}</h3>
          <p>${i(on(r))}</p>
        </div>
      </div>
      <div class="pill-row mb-2">
        ${W(r.health??r.status,r.displayStatus??o(r.kind),r.label,r.description,`topology:${O.selectedPartition}:${r.id}`)}
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
  `}var Wt={overviewPanel:{eyebrow:`Control center`,title:`Control Center`},topologyPanel:{eyebrow:`Deployment graph`,title:`Topology`},rolloutsPanel:{eyebrow:`Release timeline`,title:`Rollouts`},historyPanel:{eyebrow:`Push timeline`,title:`History`}};function U(){let e=O.activePanel,t=O.detail,r=Wt[e]??Wt.overviewPanel,a=e===`overviewPanel`,o=!!O.selectedPartition,s=a&&!o,c=(e,t)=>{let n=document.getElementById(e);n&&(n.style.display=t?``:`none`)};c(`appGridSection`,s),c(`summaryGrid`,s),c(`selectedPartitionHero`,a&&o),c(`sidebarPartitionSection`,!0),n(`pageEyebrow`,r.eyebrow),n(`pageTitle`,r.title);let l=`Monitor partitions, inspect topology, and review history.`,u=``;if(e===`overviewPanel`&&t){l=`${t.partition.manifest.spec?.deletionPolicy??`orphan`} policy · ${t.partition.manifest.spec?.reconciliation?.mode??`manual`} reconcile · ${t.intents.length} intents`;let e=qt(t),n=W(e.status,e.label,`${t.partition.manifest.metadata.name} snapshot freshness`,e.detail,`partition-freshness:${t.partition.manifest.metadata.name}`);u=`${W(t.health?.status,t.health?.displayStatus??`Selected`,`${t.partition.manifest.metadata.name} health`,t.health?.summary,`partition-health:${t.partition.manifest.metadata.name}`)} ${n} <span class="pill">${t.topology?.nodes?.length??0} nodes</span>`}e===`topologyPanel`&&(l=t?`Topology for ${t.partition.manifest.metadata.name}.`:`Select a partition to inspect its graph.`,u=t?`<span class="pill">${i(t.partition.manifest.metadata.name)}</span><span class="pill">${t.topology?.nodes?.length??0} nodes</span>`:``),e===`rolloutsPanel`&&(l=t?`Review archived rollout changes for ${t.partition.manifest.metadata.name}.`:`Select a partition to inspect rollout history.`,u=t?`<span class="pill">${i(t.partition.manifest.metadata.name)}</span><span class="pill">${O.rollouts?.rollouts?.length??0} rollouts</span>`:``),e===`historyPanel`&&(l=t?`Review deployments and state changes for ${t.partition.manifest.metadata.name}.`:`Select a partition to inspect history.`,u=t?`<span class="pill">${i(t.partition.manifest.metadata.name)}</span><span class="pill">${O.history?.deployments?.length??0} deployments</span>`:``),n(`pageSubtitle`,l);let d=document.getElementById(`headerContextPills`);d&&(d.innerHTML=u,d.style.display=u.trim()?``:`none`);let f=document.getElementById(`topnavPartition`);f&&(f.textContent=O.selectedPartition||t?.partition?.manifest?.metadata?.name||r.title||`Control Center`)}function W(e,t,n,r,s,c){let l=String(e??`neutral`).toLowerCase(),u=t??o(l),d=Jt(s,l,r),f=(l===`failing`||l===`attention`||l===`drifted`||l===`drifted-locked`)&&d.length>0,p=c?` title="${a(c)}"`:``;return f?`<button type="button" class="badge badge-${a(l)} badge-clickable" data-diagnostic-title="${a((n??u).trim())}" data-diagnostic-detail="${a(d)}" aria-label="Show diagnostic details for ${a(u)}"${p}>${i(u)}</button>`:`<span class="badge badge-${a(l)}"${p}>${i(u)}</span>`}function G(e){if(!e)return null;let t=String(e);if(t===`0001-01-01T00:00:00Z`||t===``)return null;let n=Date.parse(t);return Number.isFinite(n)?n:null}function K(e){if(!e)return null;let t=Date.parse(String(e));return Number.isFinite(t)?t:null}function Gt(e){let t=e?.state?.timestamps??{},n=[G(t.lastCheckAt),G(t.lastApplyAt),G(t.lastDiffAt),G(t.lastQueuedAt)].filter(e=>e!==null);return n.length===0?null:Math.max(...n)}function q(e){let t=Math.max(0,Math.floor(e/1e3));if(t<60)return`${t}s ago`;let n=Math.floor(t/60);if(n<60)return`${n}m ago`;let r=Math.floor(n/60);return r<24?`${r}h ago`:`${Math.floor(r/24)}d ago`}function Kt(e){let t=Gt(e);if(t===null)return{status:`neutral`,label:`No sample`,detail:`No reconciliation timestamps are available yet for this intent.`};let n=Date.now()-t,r=n>Ve;return{status:r?`attention`:`healthy`,label:r?`Stale ${q(n)}`:`Fresh ${q(n)}`,detail:`Last sampled ${q(n)} at ${c(new Date(t).toISOString())}.`}}function qt(e){let t=(Array.isArray(e?.intents)?e.intents:[]).map(e=>Gt(e)).filter(e=>e!==null);if(t.length===0)return{status:`neutral`,label:`No samples`,detail:`No intent reconciliation timestamps are available for this partition.`};let n=Math.max(...t),r=Date.now()-n,i=r>Ve;return{status:i?`attention`:`healthy`,label:i?`Stale snapshot ${q(r)}`:`Fresh snapshot ${q(r)}`,detail:`Most recent intent sample is ${q(r)} old (${c(new Date(n).toISOString())}).`}}function Jt(e,t,n){let r=String(e??``).trim(),i=String(n??``).trim();return r?t===`failing`||t===`attention`||t===`drifted`||t===`drifted-locked`?i?(O.diagnosticDetails[r]=i,i):O.diagnosticDetails[r]??``:(delete O.diagnosticDetails[r],i):i}function Yt(e){return e.map(e=>String(e??``).trim()).filter(e=>e.length>0).join(`
`)}function Xt(){let e=document.getElementById(`diagnosticsModal`);if(e)return e;let t=document.createElement(`div`);return t.id=`diagnosticsModal`,t.className=`diagnostics-modal hidden`,t.innerHTML=`
    <div class="diagnostics-modal-card" role="dialog" aria-modal="true" aria-labelledby="diagnosticsModalTitle">
      <div class="diagnostics-modal-header">
        <h3 id="diagnosticsModalTitle">Status details</h3>
        <button type="button" class="diagnostics-close" data-diagnostics-close="true" aria-label="Close diagnostics">×</button>
      </div>
      <pre id="diagnosticsModalBody" class="diagnostics-modal-body"></pre>
    </div>
  `,t.addEventListener(`click`,e=>{let n=e.target;(n===t||n.closest(`[data-diagnostics-close='true']`))&&Qt()}),document.body.appendChild(t),t}function Zt(e,t){let n=Xt(),r=n.querySelector(`#diagnosticsModalTitle`),i=n.querySelector(`#diagnosticsModalBody`);r&&(r.textContent=e.trim()||`Status details`),i&&(i.textContent=t.trim()),n.classList.remove(`hidden`),document.body.classList.add(`diagnostics-open`)}function Qt(){let e=document.getElementById(`diagnosticsModal`);e&&(e.classList.add(`hidden`),document.body.classList.remove(`diagnostics-open`))}function $t(e){let t=e.partition?.manifest?.spec?.reconciliation?.mode??`manual`,n=e.partition?.manifest?.spec?.labels?.managedBy??``,r=(e.intents??[]).some(e=>e.targetSummary&&e.targetSummary!==`Unassigned`),i=e.health?.pending??0,a=[];return a.push(`${i} asset${i===1?` is`:`s are`} in <strong>Planned</strong> state — no reconcile has run yet.`),n===`external-compose`&&a.push(`Resources in this partition are managed externally by Docker Compose.`),t===`manual`?r?a.push(`Click <strong>Reconcile now</strong> in the sidebar to run the first reconcile and deploy assets.`):a.push(`No pusher is assigned. Assets will stay in Planned state until a pusher is configured.`):a.push(`The reconciler will process these assets automatically in the next cycle.`),`<div class="info-callout mt-2"><span class="info-callout-icon">?</span><div><strong>Why is this partition Progressing?</strong><p>${a.join(` `)}</p></div></div>`}function en(e){let t=[`component`,`role`,`stack`,`topology`],n=[];return t.forEach(t=>{e[t]&&n.push(e[t])}),[...new Set(n)]}function tn(e){return(O.detail?.intents??[]).find(t=>t.name===e)??null}function nn(e,t){return tn(e)?.assets?.find(e=>e.name===t)??null}var J={partition:`#F0E442`,intent:`#CC79A7`,runtime:`#0072B2`,config:`#009E73`,storage:`#56B4E9`,traffic:`#D55E00`,muted:`#8B949E`};function rn(e){return e.kind===`partition`?J.partition:e.kind===`intent`?J.intent:yn(e.assetType??e.kind)}function an(e){return e.kind===`partition`?`◫`:e.kind===`intent`?`⊞`:vn(e.assetType??e.kind)}function on(e){return e.kind===`partition`?`${e.meta?.reconciliation??`manual`} reconcile · ${e.meta?.deletionPolicy??`orphan`} delete`:e.kind===`intent`?e.meta?.target??e.displayStatus??`Intent`:`${Q(e.assetType??e.kind)} · ${e.displayStatus??`Asset`}`}var Y=[`Compute`,`Network`,`Config`,`Storage`,`Operations`],sn={Compute:J.runtime,Volume:J.storage,Config:J.config,ObjectStore:J.storage,Database:J.traffic,SQLDatabase:J.traffic,LoadBalancer:J.traffic,Observability:J.config},cn={Image:`Container image reference (registry/name:tag@digest)`,Scale:`Desired replica count`,Env:`Environment variables injected at runtime`,Config:`ConfigMap or file mounts`,Storage:`Persistent volume mounts`,Ports:`Exposed service ports`,Port:`Service listener port`,Health:`Health check probe is configured`,CPU:`CPU resource limit/request`,Memory:`Memory resource limit/request`,Engine:`Storage engine or database type`,Version:`Engine version`,Database:`Database name`,Mode:`Deployment or storage mode`,Endpoint:`Connection endpoint address`,Size:`Volume storage capacity`,Access:`Volume access mode (e.g. ReadWriteOnce)`,Format:`Config file format (yaml / json / env)`,Files:`Config file definitions`,Inline:`Config data is defined inline in the manifest`,Targets:`Number of load balancer backend targets`,Listeners:`Number of load balancer listeners`,Buckets:`Object storage bucket names`,Provider:`Observability provider`,Receivers:`Telemetry input protocols`,Exporters:`Telemetry export destinations`,Outputs:`Output keys exposed to dependent intents`},ln={Scale:`fact-scale`,Ports:`fact-port`,Port:`fact-port`,CPU:`fact-resource`,Memory:`fact-resource`,Env:`fact-env`,Storage:`fact-storage`,Size:`fact-storage`,Engine:`fact-engine`,Version:`fact-engine`,Outputs:`fact-outputs`};function un(e){return e===`Release`?`fact-release`:ln[e]?` ${ln[e]}`:``}function dn(e){return cn[e]??e}function X(e){return(O.catalog?.assetTypes??[]).find(t=>t.type===e)??null}function fn(e){return e.replace(/\[\d+\]/g,`[]`)}function Z(e,t){let n=fn(t),r=n.replace(/\[\]/g,``).split(`.`)[0];return(e??[]).find(e=>e.path===n||e.path===r)??null}function pn(e,t){let n=X(e?.type??``);return Z(e?.hints,t)??Z(n?.hints,t)??Z(n?.fields,t)??null}function mn(e,t=``){if(e==null||e===``)return[];if(Array.isArray(e))return e.length?e.flatMap((e,n)=>mn(e,`${t}[${n}]`)):t?[{path:t,value:`[]`}]:[];if(typeof e==`object`){let n=Object.entries(e);return n.length?n.flatMap(([e,n])=>mn(n,t?`${t}.${e}`:e)):t?[{path:t,value:`{}`}]:[]}return t?[{path:t,value:String(e)}]:[]}function hn(e,t={}){let n=t.limit??16,r=t.truncateAt??48,o=X(e?.type??``),c=new Map;(o?.fields??[]).forEach((e,t)=>{c.set(e.path,t)});let l=mn(e?.properties??{}).sort((e,t)=>{let n=fn(e.path),r=fn(t.path),i=n.replace(/\[\]/g,``).split(`.`)[0],a=r.replace(/\[\]/g,``).split(`.`)[0],o=c.get(n)??c.get(i)??2**53-1,s=c.get(r)??c.get(a)??2**53-1;return o===s?e.path.localeCompare(t.path):o-s});if(!l.length)return``;let u=l.slice(0,n),d=u.map(t=>{let n=pn(e,t.path);return`<span class="fact" title="${a([t.path,n?.title,n?.description].filter(Boolean).join(` - `))}">${i(`${t.path}: ${s(t.value,r)}`)}</span>`}).join(``);if(l.length===u.length)return d;let f=l.length-u.length;return`${d}<span class="fact" title="${f} more properties available">+${f} more</span>`}function gn(e,t=[],n={}){let r=Object.entries(e??{}),o=n.limit??r.length,c=n.truncateAt??2**53-1,l=r.slice(0,o).map(([e,n])=>{let r=Z(t,`outputs.${e}`);return`<span class="fact" title="${a([`outputs.${e}`,r?.title,r?.description].filter(Boolean).join(` - `))}">${i(`${e}: ${s(String(n),c)}`)}</span>`}).join(``);if(r.length<=o)return l;let u=r.length-o;return`${l}<span class="fact" title="${u} more outputs available">+${u} more</span>`}function _n(e){return X(e)?.category??`Other`}function Q(e){return X(e)?.title??o(e)}function vn(e){return X(e)?.icon??`⬡`}function yn(e){return sn[e]??J.muted}function bn(e){let t={Compute:J.runtime,Network:J.traffic,Config:J.config,Storage:J.storage,Operations:J.config,Other:J.muted};return t[e]??t.Other}function xn(e){let t=new Map;return e.forEach(e=>{let n=_n(e.type);t.set(n,(t.get(n)??0)+1)}),[...t.keys()].sort((e,t)=>{let n=Y.indexOf(e),r=Y.indexOf(t);return n===-1&&r===-1?e.localeCompare(t):n===-1?1:r===-1?-1:n-r}).map(e=>({category:e,count:t.get(e)}))}function Sn(e){let t=new Map;return e.forEach(e=>{let n=_n(e.type);t.has(n)||t.set(n,[]),t.get(n).push(e)}),[...t.keys()].sort((e,t)=>{let n=Y.indexOf(e),r=Y.indexOf(t);return n===-1&&r===-1?e.localeCompare(t):n===-1?1:r===-1?-1:n-r}).map(e=>({category:e,assets:t.get(e).sort((e,t)=>e.name.localeCompare(t.name))}))}function Cn(){document.querySelectorAll(`[data-tab-target]`).forEach(e=>{e.addEventListener(`click`,()=>Je(e.dataset.tabTarget))}),de($),document.getElementById(`partitionSearch`)?.addEventListener(`input`,ot),document.getElementById(`refreshButton`)?.addEventListener(`click`,()=>j(!0).catch($)),document.getElementById(`reconcileButton`)?.addEventListener(`click`,Tn),document.getElementById(`createPartitionButton`)?.addEventListener(`click`,()=>d(`Create partition via guardianctl partition push`,`success`)),document.getElementById(`overviewCreatePartitionButton`)?.addEventListener(`click`,()=>d(`Create partition via guardianctl partition push`,`success`)),document.getElementById(`appGridSearch`)?.addEventListener(`input`,st),document.getElementById(`historyFilter`)?.addEventListener(`input`,L),document.getElementById(`historyGroupToggle`)?.addEventListener(`change`,L),document.getElementById(`historyApply`)?.addEventListener(`click`,()=>et().catch($)),document.getElementById(`historyReset`)?.addEventListener(`click`,()=>tt().catch($)),document.getElementById(`refreshRolloutsButton`)?.addEventListener(`click`,()=>{P(!0).catch($),A()}),[`showContainEdges`,`showJoinEdges`,`showAssetEdges`,`showOutputEdges`].forEach(e=>{document.getElementById(e)?.addEventListener(`change`,H)}),document.getElementById(`topologyZoomOut`)?.addEventListener(`click`,()=>En(-.1)),document.getElementById(`topologyZoomIn`)?.addEventListener(`click`,()=>En(.1)),document.getElementById(`topologyResetLayout`)?.addEventListener(`click`,()=>{O.topology.nodePositions={},H()}),document.addEventListener(`click`,e=>{let t=e.target.closest(`[data-diagnostic-detail]`);t&&(e.preventDefault(),Zt(t.dataset.diagnosticTitle??`Status details`,t.dataset.diagnosticDetail??`No diagnostic details were provided.`))}),document.addEventListener(`keydown`,e=>{e.key===`Escape`&&Qt()}),Xt(),wn()}function wn(){let e=document.getElementById(`refreshSlider`),t=document.getElementById(`refreshIntervalLabel`),n=document.getElementById(`refreshPopover`),r=document.getElementById(`syncIndicator`);if(!e||!n||!r)return;let i=Math.round(O.refreshIntervalMs/1e3);e.value=String(Math.min(Ie/1e3,Math.max(Fe/1e3,i))),t&&(t.textContent=`${e.value}s`);function a(){n.classList.contains(`hidden`)?n.classList.remove(`hidden`):n.classList.add(`hidden`)}r.addEventListener(`click`,e=>{e.stopPropagation(),a()}),e.addEventListener(`input`,()=>{let n=Number(e.value);t&&(t.textContent=`${n}s`)}),e.addEventListener(`change`,()=>{let t=Number(e.value);if(!(!Number.isFinite(t)||t<1)){O.refreshIntervalMs=t*1e3;try{localStorage.setItem(Pe,String(O.refreshIntervalMs))}catch{}Ue()}}),document.addEventListener(`click`,e=>{if(n.classList.contains(`hidden`))return;let t=e.target;!n.contains(t)&&t!==r&&n.classList.add(`hidden`)})}async function Tn(){let e=O.selectedPartition;if(!e){d(`Select a partition first.`,`error`);return}await t(`/api/partitions/${encodeURIComponent(e)}/reconcile`,{method:`POST`}),Ke(),d(`Reconciliation requested.`,`success`),await j(!1),await M(e,!1)}function En(e){let t=document.getElementById(`topologyCanvas`),n=On(O.topology.zoom,.4,2.5),r=On(Math.round((n+e)*100)/100,.4,2.5);if(n===r)return;let i=t?t.scrollLeft+t.clientWidth/2:0,a=t?t.scrollTop+t.clientHeight/2:0;if(O.topology.zoom=r,H(),t){let e=r/n;t.scrollLeft=Math.max(0,i*e-t.clientWidth/2),t.scrollTop=Math.max(0,a*e-t.clientHeight/2)}Dn()}function Dn(){let e=O.topology.zoom,t=document.getElementById(`topologyZoomOut`),n=document.getElementById(`topologyZoomIn`),r=document.getElementById(`topologyZoomValue`);r&&(r.textContent=`${Math.round(e*100)}%`),t&&(t.disabled=e<=.4),n&&(n.disabled=e>=2.5)}function On(e,t,n){return Math.min(n,Math.max(t,e))}function $(e){d(e?.message??`Unexpected error`,`error`)}async function kn(){try{O.catalog=await t(`/api/catalog`)}catch{}}kn();