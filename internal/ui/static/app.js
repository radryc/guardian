(function(){let e=document.createElement(`link`).relList;if(e&&e.supports&&e.supports(`modulepreload`))return;for(let e of document.querySelectorAll(`link[rel="modulepreload"]`))n(e);new MutationObserver(e=>{for(let t of e)if(t.type===`childList`)for(let e of t.addedNodes)e.tagName===`LINK`&&e.rel===`modulepreload`&&n(e)}).observe(document,{childList:!0,subtree:!0});function t(e){let t={};return e.integrity&&(t.integrity=e.integrity),e.referrerPolicy&&(t.referrerPolicy=e.referrerPolicy),e.crossOrigin===`use-credentials`?t.credentials=`include`:e.crossOrigin===`anonymous`?t.credentials=`omit`:t.credentials=`same-origin`,t}function n(e){if(e.ep)return;e.ep=!0;let n=t(e);fetch(e.href,n)}})();function e(){return{activePanel:`overviewPanel`,selectedPartition:``,selectedSet:``,overview:null,detail:null,expandedAssetKey:``,expandedRolloutKeys:{},history:null,historyLoading:!1,historyError:``,rollouts:null,rolloutsLoading:!1,rolloutsError:``,catalog:null,topology:{zoom:1,nodePositions:{},selectedNodeId:``},activityDrawer:{intentName:``,data:null,loading:!1,error:``},historyOptions:{limit:10,since:``,until:``},refreshTimer:void 0,rolloutsRefreshTimer:void 0,fastRefreshUntil:0,refreshIntervalMs:2e4,diagnosticDetails:{}}}async function t(e,t){let n=await fetch(e,{headers:{Accept:`application/json`,...t?.headers??{}},...t});if(!n.ok){let e=`HTTP ${n.status}`;try{let t=await n.json();e=t.error??t.message??e}catch{}throw Error(e)}return n.status===204?null:n.json()}function n(e,t){let n=document.getElementById(e);n&&(n.textContent=String(t))}function r(e){let t=document.getElementById(`syncIndicator`);t&&(t.textContent=e)}function i(e){return e.replace(/&/g,`&amp;`).replace(/</g,`&lt;`).replace(/>/g,`&gt;`).replace(/"/g,`&quot;`).replace(/'/g,`&#x27;`)}function a(e){return e.replace(/[^a-zA-Z0-9_-]/g,e=>`&#${e.charCodeAt(0)};`)}function o(e){return e.replace(/[-_]/g,` `).replace(/([a-z])([A-Z])/g,`$1 $2`).replace(/\b\w/g,e=>e.toUpperCase())}function s(e,t){return e.length<=t?e:`${e.slice(0,t-1)}…`}function c(e){if(!e||e===`0001-01-01T00:00:00Z`)return`—`;let t=new Date(e),n=Math.floor((Date.now()-t.getTime())/1e3);return n<60?`${n}s ago`:n<3600?`${Math.floor(n/60)}m ago`:n<86400?`${Math.floor(n/3600)}h ago`:t.toLocaleDateString(void 0,{month:`short`,day:`numeric`,year:`numeric`})}function l(e){if(!e)return``;let t=new Date(e);if(Number.isNaN(t.getTime()))return``;let n=e=>String(e).padStart(2,`0`);return`${t.getFullYear()}-${n(t.getMonth()+1)}-${n(t.getDate())}T${n(t.getHours())}:${n(t.getMinutes())}`}function u(e){let t=e.trim();if(!t)return``;let n=new Date(t);return Number.isNaN(n.getTime())?``:n.toISOString()}function d(e,t=`info`){let n=document.getElementById(`toastContainer`);if(!n)return;let r=document.createElement(`div`);r.className=`toast toast-${t}`,r.setAttribute(`role`,`status`),r.setAttribute(`aria-live`,`polite`),r.textContent=e,n.appendChild(r),requestAnimationFrame(()=>r.classList.add(`toast-visible`)),setTimeout(()=>{r.classList.remove(`toast-visible`),r.addEventListener(`transitionend`,()=>r.remove(),{once:!0}),setTimeout(()=>r.remove(),400)},3600)}var f={partition:`#F0E442`,intent:`#CC79A7`,runtime:`#0072B2`,config:`#009E73`,storage:`#56B4E9`,traffic:`#D55E00`,muted:`#8B949E`},p={healthy:`#00C369`,attention:`#FCB519`,failing:`#EE5F54`,pending:`#00ADE4`,drifted:`#FCB519`,"drifted-locked":`#F5A623`,neutral:`#566778`};function m(e){return e.kind===`partition`?200:e.kind===`intent`?230:220}function h(e){return e.kind===`partition`?76:72}function ee(e){return e.kind===`partition`?f.partition:e.kind===`intent`?f.intent:{Compute:f.runtime,Volume:f.storage,Config:f.config,ObjectStore:f.storage,Database:f.traffic,SQLDatabase:f.traffic,LoadBalancer:f.traffic,Observability:f.config}[e.assetType??``]??f.muted}function te(e){return e.kind===`partition`?`◫`:e.kind===`intent`?`⊞`:{Compute:`⧖`,Volume:`⊠`,Config:`≡`,ObjectStore:`⬜`,Database:`⫿`,SQLDatabase:`⫿`,LoadBalancer:`⊷`,Observability:`◎`}[e.assetType??``]??`⬡`}function ne(e){return e.kind===`partition`?`${e.meta?.reconciliation??`manual`} reconcile · ${e.meta?.deletionPolicy??`orphan`} delete`:e.kind===`intent`?e.meta?.target??e.displayStatus??`Intent`:`${e.assetType??`Asset`} · ${e.displayStatus??`Asset`}`}function re(e){return p[e.health??e.status??`neutral`]??p.neutral}function ie(e,t){let n=e.find(e=>e.kind===`partition`),r=e.filter(e=>e.kind===`intent`).sort((e,t)=>Number(e.level)-Number(t.level)||e.label.localeCompare(t.label)),i=e.filter(e=>e.kind===`asset`).sort((e,t)=>(e.parentID??``).localeCompare(t.parentID??``)||Number(e.level)-Number(t.level)||e.label.localeCompare(t.label)),a={},o=new Map;r.forEach(e=>{let t=Number(e.level??1);o.has(t)||o.set(t,[]),o.get(t).push(e)});let s=70;if([...o.keys()].sort((e,t)=>e-t).forEach(e=>{o.get(e).forEach(t=>{a[t.id]={x:260+(e-1)*320,y:s},s+=154}),s+=24}),n){let e=r.map(e=>a[e.id]).filter(Boolean),t=e.length?Math.min(...e.map(e=>e.y)):90,i=e.length?Math.max(...e.map(e=>e.y)):90;a[n.id]={x:40,y:Math.round((t+i)/2)}}r.forEach(e=>{let t=a[e.id];if(!t)return;let n=new Map;i.filter(t=>t.parentID===e.id).forEach(t=>{let r=Math.max(0,Number(t.level??0)-Number(e.level??0)-2);n.has(r)||n.set(r,[]),n.get(r).push(t)}),[...n.keys()].sort((e,t)=>e-t).forEach(e=>{let r=n.get(e),i=Math.max(0,(r.length-1)*96);r.forEach((n,r)=>{a[n.id]={x:t.x+280+e*250,y:t.y-i/2+r*96+e*8}})})});let c=Math.min(...Object.values(a).map(e=>e.y),40);if(c<40){let e=40-c;Object.values(a).forEach(t=>{t.y+=e})}return Object.entries(t).forEach(([e,t])=>{a[e]&&(a[e]={x:t.x,y:t.y})}),a}function ae(e,t,n,r){let i=e.x+m(n),a=e.y+h(n)/2,o=t.x,s=t.y+h(r)/2,c=o-i,l=c>=0?1:-1,u=Math.max(70,Math.abs(c)/2);return`M ${i} ${a} C ${i+u*l} ${a}, ${o-u*l} ${s}, ${o} ${s}`}function oe(e,t,n,r){e.querySelectorAll(`path.topology-edge`).forEach((e,i)=>{let a=n[i];if(!a)return;let o=t[a.from],s=t[a.to],c=r.get(a.from),l=r.get(a.to);o&&s&&c&&l&&e.setAttribute(`d`,ae(o,s,c,l))})}function se(e){let{canvas:t,topology:n,zoom:r,savedPositions:o,selectedNodeId:c,filters:l,onSelectNode:u,onDragNode:d}=e;if(!n?.nodes?.length){t.innerHTML=`<p class="empty-state" style="padding:24px">Select a partition to visualize its topology.</p>`;return}let f=n.nodes,p=new Map(f.map(e=>[e.id,e])),se=(n.edges??[]).filter(e=>l[e.kind]!==!1),g=ie(f,o),ce=p.get(c)??f.find(e=>e.kind===`intent`)??f[0],le=Math.max(...Object.values(g).map(e=>e.x+260),400),ue=Math.max(...Object.values(g).map(e=>e.y+100),260),de=le+40,fe=ue+40,_=[`<div class="topology-svg-frame">`,`<svg class="topology-svg" viewBox="0 0 ${de} ${fe}" width="${Math.round(de*r)}" height="${Math.round(fe*r)}" xmlns="http://www.w3.org/2000/svg">`,`<defs><filter id="ts"><feDropShadow dx="0" dy="6" stdDeviation="10" flood-color="rgba(0,0,0,0.28)"/></filter></defs>`];se.forEach(e=>{let t=g[e.from],n=g[e.to],r=p.get(e.from),o=p.get(e.to);if(!(!t||!n||!r||!o)&&(_.push(`<path class="topology-edge ${a(e.kind)}" d="${ae(t,n,r,o)}" />`),e.label)){let a=t.x+m(r),s=t.y+h(r)/2,c=n.x,l=n.y+h(o)/2;_.push(`<text class="topology-edge-label" x="${(a+c)/2}" y="${(s+l)/2-10}">${i(e.label)}</text>`)}}),f.forEach(e=>{let t=g[e.id];if(!t)return;let n=ee(e),r=ce?.id===e.id,o=m(e),c=h(e);_.push(`
      <g class="topology-node ${a(e.kind)}${r?` selected`:``}" data-node="${a(e.id)}" transform="translate(${t.x},${t.y})">
        <rect class="topology-node-card" width="${o}" height="${c}" rx="12" filter="url(#ts)" />
        <rect class="topology-node-accent" width="4" height="${c}" rx="4" fill="${n}" />
        <circle cx="18" cy="18" r="5.5" fill="${re(e)}" />
        <text x="32" y="20" class="topology-node-title">${i(`${te(e)} ${e.label}`)}</text>
        <text x="32" y="38" class="topology-node-subtitle">${i(ne(e))}</text>
        <text x="14" y="60" class="topology-node-description">${i(s(e.description??``,68))}</text>
      </g>
    `)}),_.push(`</svg>`,`</div>`),t.innerHTML=_.join(``);let v=t.querySelector(`svg.topology-svg`);v.querySelectorAll(`[data-node]`).forEach(e=>{let t=e.dataset.node;function n(e,t){let n=v.createSVGPoint();n.x=e,n.y=t;let r=v.getScreenCTM();if(!r)return{x:e,y:t};let i=n.matrixTransform(r.inverse());return{x:i.x,y:i.y}}let r=!1,i=!1,a=0,o=0,s=0,c=0;e.addEventListener(`pointerdown`,n=>{if(n.button!==0)return;r=!0,i=!1,a=n.clientX,o=n.clientY;let l=g[t];s=l?l.x:0,c=l?l.y:0,e.setPointerCapture(n.pointerId),n.stopPropagation()}),e.addEventListener(`pointermove`,l=>{if(!r)return;let u=n(a,o),f=n(l.clientX,l.clientY),m=s+(f.x-u.x),h=c+(f.y-u.y);!i&&Math.abs(m-s)<3&&Math.abs(h-c)<3||(i=!0,g[t]={x:m,y:h},e.setAttribute(`transform`,`translate(${m},${h})`),e.classList.add(`dragging`),oe(v,g,se,p),d(t,{...g}),l.stopPropagation())}),e.addEventListener(`pointerup`,n=>{r&&(r=!1,e.releasePointerCapture(n.pointerId),e.classList.remove(`dragging`),i&&d(t,{...g}),n.stopPropagation())}),e.addEventListener(`click`,e=>{if(i){e.stopPropagation(),e.preventDefault();return}u(t,{...g})})})}function g(e){e&&(e.innerHTML=`
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
  `)}var ce=3e3,le=200,ue=()=>void 0,de=[],fe=[],_=``,v=null,y=null,b={},pe={},me,he=!1,ge=!1,_e=``;function ve(e){ue=e,document.getElementById(`scanStartButton`)?.addEventListener(`click`,()=>ke().catch(ue)),document.getElementById(`scanGenerateButton`)?.addEventListener(`click`,()=>He().catch(ue)),document.getElementById(`scanList`)?.addEventListener(`click`,e=>{let t=e.target.closest(`[data-scan-select]`);t&&Ae(t.dataset.scanSelect??``).catch(ue)}),document.getElementById(`scanBundleResult`)?.addEventListener(`change`,e=>{let t=e.target;if(!(t instanceof HTMLInputElement))return;let n=t.dataset.intentToggle;n&&(b[n]=t.checked,Ue())}),document.getElementById(`scanBundleResult`)?.addEventListener(`click`,e=>{let t=e.target.closest(`[data-scan-save]`);t&&Ge(t.dataset.scanSave===`reconcile`).catch(ue)}),document.getElementById(`scanStackSelect`)?.addEventListener(`change`,e=>{let t=e.target;if(!(t instanceof HTMLInputElement))return;let n=t.dataset.scanStack;n&&(pe[n]=t.checked,Pe())}),document.getElementById(`scanStackAll`)?.addEventListener(`click`,()=>Fe(!0)),document.getElementById(`scanStackNone`)?.addEventListener(`click`,()=>Fe(!1))}function ye(e={}){if(be(e),he=!document.getElementById(`scanPanel`)?.classList.contains(`hidden`),!he){xe();return}de.length===0&&fe.length===0?Te().catch(ue):Oe(),Se()}function be(e){let t=document.getElementById(`scanPartitionName`);if(!t)return;let n=t.value.trim(),r=!n||n===_e;if(e.resetTarget){r&&(t.value=``,_e=``);return}let i=e.targetPartition?.trim()??``;i&&r&&(t.value=i,_e=i)}function xe(){me!==void 0&&(window.clearTimeout(me),me=void 0)}function Se(){if(me!==void 0)return;let e=async()=>{if(me=void 0,he){try{await Te(),_&&we()&&await je()}catch{}he&&Ce()&&(me=window.setTimeout(e,ce))}};Ce()&&(me=window.setTimeout(e,ce))}function Ce(){return fe.some(e=>e.status===`Queued`||e.status===`Running`)}function we(){return!v||v.status===`Queued`||v.status===`Running`}async function Te(){let e=await t(`/api/scans`);de=(e.pushers??[]).filter(e=>e.includes(`aws`)),fe=e.scans??[],Ee(),Oe()}function Ee(){let e=document.getElementById(`scanPusher`);if(!e)return;let t=e.value;e.innerHTML=de.length===0?`<option value="">No AWS pushers configured</option>`:de.map(e=>`<option value="${a(e)}">${i(e)}</option>`).join(``),t&&de.includes(t)&&(e.value=t)}function De(e){let t=String(e??``).toLowerCase();return t===`succeeded`?`badge badge-healthy`:t===`failed`?`badge badge-failing`:t===`running`?`badge badge-pending`:`badge badge-neutral`}function Oe(){let e=document.getElementById(`scanList`);if(e){if(fe.length===0){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No scans yet. Start one above.`;return}e.className=`grid gap-1.5`,e.innerHTML=fe.map(e=>{let t=e.summary??{},n=(e.regions??[]).join(`, `)||`—`,r=e.createdAt?c(e.createdAt):`—`,o=[`${t.bucketCount??0} buckets`,`${t.serviceCount??0} services`,`${t.loadBalancerCount??0} LBs`,`${t.stackCount??0} stacks`,`${t.inventoryCount??0} inventory`].join(` · `);return`
      <div class="flex items-center gap-3 px-3.5 py-2.5 rounded-lg border ${e.scanID===_?`border-[#00ADE4]/50 bg-[#00ADE4]/[0.06]`:`border-white/[0.07] bg-[#0D1220]`}">
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2 flex-wrap">
            <span class="text-[13px] font-semibold text-[#E5ECF4] truncate">${i(e.pusher)}</span>
            <span class="${De(e.status)}">${i(e.status??``)}</span>
            <span class="text-[11px] text-[#566778]">${i(r)}</span>
          </div>
          <div class="text-[12px] text-[#9BB0CF] mt-0.5 truncate">${i(n)} · ${i(o)}</div>
        </div>
        <button class="btn-ghost shrink-0" data-scan-select="${a(e.scanID)}">${e.status===`Succeeded`?`Inspect`:`View`}</button>
      </div>`}).join(``)}}async function ke(){let e=document.getElementById(`scanPusher`)?.value?.trim()??``;if(!e){d(`Select an AWS pusher first.`,`error`);return}let n=document.getElementById(`scanRegions`)?.value?.trim()??``,r=document.getElementById(`scanIncludeTypes`)?.value?.trim()??``,i=document.getElementById(`scanExcludeTypes`)?.value?.trim()??``,a={pusher:e,inventory:document.getElementById(`scanInventory`)?.checked??!1};n&&(a.regions=n.split(`,`).map(e=>e.trim()).filter(Boolean)),r&&(a.includeResourceTypes=r.split(`,`).map(e=>e.trim()).filter(Boolean)),i&&(a.excludeResourceTypes=i.split(`,`).map(e=>e.trim()).filter(Boolean)),_=(await t(`/api/scans`,{method:`POST`,headers:{"Content-Type":`application/json`},body:JSON.stringify(a)})).scanID??``,v=null,y=null,d(`Scan queued. The AWS pusher will pick it up shortly.`,`success`),await Te(),Se()}async function Ae(e){e&&(_=e,y=null,b={},pe={},await je(),Oe())}async function je(){_&&(v=await t(`/api/scans/${encodeURIComponent(_)}`),Me())}function x(e,t){return`<span class="pill">${i(e)} <strong>${t??0}</strong></span>`}function Me(){let e=document.getElementById(`scanDetailWrap`);if(!e||!v)return;let t=v.status===`Succeeded`||v.status===`Failed`;if(e.classList.toggle(`hidden`,!t),!t){let e=document.getElementById(`scanDetailTitle`);e&&(e.textContent=`Scan ${_} is ${v.status??`pending`}`);return}let n=document.getElementById(`scanDetailTitle`);n&&(n.textContent=`Scan ${_}`);let r=document.getElementById(`scanDetailSubtitle`);r&&(r.textContent=[v.account?`account ${v.account}`:``,Array.isArray(v.regions)?v.regions.join(`, `):``].filter(Boolean).join(` · `));let i=v.summary??{},a=document.getElementById(`scanSummary`);a&&(a.innerHTML=[x(`Regions`,i.regionCount),x(`Buckets`,i.bucketCount),x(`EFS`,i.fileSystemCount),x(`Params`,i.parameterCount),x(`Secrets`,i.secretCount),x(`Services`,i.serviceCount),x(`LBs`,i.loadBalancerCount),x(`Stacks`,i.stackCount),x(`Inventory`,i.inventoryCount),x(`Managed`,i.managedCount),x(`Errors`,i.errorCount)].join(``)),Ie(),Re(),Be(),Ve(),Pe()}function Ne(){let e={},t=t=>{for(let n of t??[]){if(n?.managed?.managed)continue;let t=String(n?.stack??``).trim();t&&(e[t]=(e[t]??0)+1)}};return t(v?.buckets),t(v?.fileSystems),t(v?.parameters),t(v?.secrets),t(v?.services),t(v?.loadBalancers),e}function Pe(){let e=document.getElementById(`scanStackSelect`);if(!e)return;let t=[...v?.stacks??[]].sort((e,t)=>String(e?.name??``).localeCompare(String(t?.name??``))),n=document.getElementById(`scanStackCount`);if(t.length===0){e.innerHTML=`<div class="text-[12px] text-[#566778] px-1">No CloudFormation stacks discovered in this scan.</div>`,n&&(n.textContent=``);return}let r=Ne(),o=t.filter(e=>!(e?.managed?.managed??!1)),s=0;e.innerHTML=t.map(e=>{let t=String(e?.name??``),n=e?.managed?.managed??!1,o=!n&&(pe[t]??!1);o&&s++;let c=r[t]??0;return`
      <label class="flex items-center gap-2.5 px-3 py-1.5 rounded border ${o?`border-[#00ADE4]/50 bg-[#00ADE4]/[0.06]`:`border-white/[0.07] bg-[#0D1220]`} ${n?`opacity-60`:`cursor-pointer`}">
        <input type="checkbox" ${o?`checked`:``} ${n?`disabled`:``} data-scan-stack="${a(t)}" class="accent-[#00ADE4]" />
        <span class="min-w-0 flex-1 flex items-center gap-2">
          <span class="text-[12px] text-[#E5ECF4] truncate">${i(t)}</span>
          ${e?.region?`<span class="badge badge-neutral">${i(String(e.region))}</span>`:``}
          <span class="text-[11px] text-[#566778] shrink-0">${c} importable</span>
          ${n?`<span class="badge badge-healthy ml-auto">managed</span>`:``}
        </span>
      </label>`}).join(``),n&&(n.textContent=`${s}/${o.length} selected — leave empty to import every stack`)}function Fe(e){for(let t of v?.stacks??[]){let n=String(t?.name??``);!n||t?.managed?.managed||(pe[n]=e)}Pe()}function Ie(){let e=document.getElementById(`scanManagedList`);if(!e)return;let t=Le(v);if(t.length===0){e.innerHTML=``;return}e.innerHTML=`
    <div class="text-[11px] font-bold uppercase tracking-[0.09em] text-[#566778] mb-1.5">Already managed (${t.length})</div>
    <div class="grid gap-1">
      ${t.slice(0,50).map(e=>`
        <div class="flex items-center gap-2 text-[12px] text-[#9BB0CF] px-3 py-1.5 rounded border border-white/[0.07] bg-[#0D1220]">
          <span class="badge badge-neutral">${i(e.kind??``)}</span>
          <span class="text-[#E5ECF4] truncate">${i(e.identifier??``)}</span>
          <span class="text-[#566778] truncate ml-auto">${i(e.partition?`${e.partition}/${e.intent}/${e.asset??``}`:e.existingIntent?`referenced by existing intent`:``)}</span>
        </div>`).join(``)}
      ${t.length>50?`<div class="text-[12px] text-[#566778] px-3">+${t.length-50} more</div>`:``}
    </div>`}function Le(e){let t=[],n=(e,n,r)=>{for(let i of n??[])i?.managed?.managed&&t.push({kind:e,identifier:r(i),partition:i.managed.partition,intent:i.managed.intent,asset:i.managed.asset})};return n(`bucket`,e?.buckets,e=>e.name),n(`fileSystem`,e?.fileSystems,e=>e.id),n(`parameter`,e?.parameters,e=>e.name),n(`secret`,e?.secrets,e=>e.name),n(`service`,e?.services,e=>`${e.clusterName??``}/${e.name??``}`),n(`loadBalancer`,e?.loadBalancers,e=>e.name),n(`stack`,e?.stacks,e=>e.name),t}function Re(){let e=document.getElementById(`scanUnmappedList`);if(!e)return;let t=ze(v);if(t.length===0){e.innerHTML=``;return}e.innerHTML=`
    <div class="text-[11px] font-bold uppercase tracking-[0.09em] text-[#566778] mb-1.5">Foreign CloudFormation stacks (${t.length})</div>
    <div class="grid gap-1">
      ${t.slice(0,25).map(e=>`
        <div class="flex items-center gap-2 text-[12px] text-[#9BB0CF] px-3 py-1.5 rounded border border-white/[0.07] bg-[#0D1220]">
          <span class="badge badge-attention">stack</span>
          <span class="text-[#E5ECF4] truncate">${i(e.name??``)}</span>
          <span class="text-[#566778] truncate ml-auto">${i(e.region??``)} · ${i(e.status??``)}</span>
        </div>`).join(``)}
      ${t.length>25?`<div class="text-[12px] text-[#566778] px-3">+${t.length-25} more</div>`:``}
    </div>`}function ze(e){return(e?.stacks??[]).filter(e=>!e?.managed?.managed)}function Be(){let e=document.getElementById(`scanInventoryList`);if(!e)return;let t=v?.inventory??{},n=Object.keys(t).sort();if(n.length===0){e.innerHTML=``;return}e.innerHTML=`
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
    </div>`}function Ve(){let e=document.getElementById(`scanErrorsList`);if(!e)return;let t=v?.errors??[];if(t.length===0){e.innerHTML=``;return}e.innerHTML=`
    <div class="text-[11px] font-bold uppercase tracking-[0.09em] text-[#566778] mb-1.5">Scan errors (${t.length})</div>
    <div class="grid gap-1 max-h-52 overflow-y-auto">
      ${t.slice(0,100).map(e=>`
        <div class="text-[12px] text-[#9BB0CF] px-3 py-1.5 rounded border border-[#E5484D]/25 bg-[#E5484D]/[0.06]">
          ${i([e.region,e.resourceType].filter(Boolean).join(` · `))} — ${i(e.message??``)}
        </div>`).join(``)}
      ${t.length>100?`<div class="text-[12px] text-[#566778] px-3">+${t.length-100} more</div>`:``}
    </div>`}async function He(){if(!_){d(`Select a finished scan first.`,`error`);return}let e=document.getElementById(`scanPartitionName`)?.value?.trim()??``;if(!e){d(`Enter a target partition name.`,`error`);return}let n=document.getElementById(`scanGrouping`)?.value??`stack`,r=new URLSearchParams({partition:e,grouping:n}),i=Object.keys(pe).filter(e=>pe[e]);i.length>0&&r.set(`stacks`,i.join(`,`));let a=await t(`/api/scans/${encodeURIComponent(_)}/bundle?${r.toString()}`);y=a,b={};for(let e of a.bundle?.intents??[])b[e.manifest?.metadata?.name??``]=!0;Ue()}function Ue(){let e=document.getElementById(`scanBundleResult`);if(!e)return;if(!y){e.innerHTML=``;return}let t=y.draft??{},n=t.warnings??[],r=t.managed??[],o=y.bundle?.intents??[];if(o.length===0){e.innerHTML=`
      <div class="empty-state text-sm text-[#566778]">
        Nothing new to import${r.length>0?` — ${r.length} scanned resources are already managed or imported.`:`.`}
      </div>`;return}let s=o.filter(e=>b[e.manifest?.metadata?.name??``]).length;e.innerHTML=`
    ${n.length>0?`
      <div class="grid gap-1 mb-3">
        ${n.map(e=>`<div class="text-[12px] text-[#FCB519] px-3 py-1.5 rounded border border-[#FCB519]/25 bg-[#FCB519]/[0.06]">${i(e)}</div>`).join(``)}
      </div>`:``}
    <div class="grid gap-1.5 mb-3">
      ${o.map(e=>{let t=e.manifest??{},n=t.metadata?.name??``,r=t.spec?.target?.region??``,o=t.spec?.assets??[],s=b[n]??!1;return`
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
      <pre class="px-3 pb-3 text-[11px] text-[#9BB0CF] overflow-x-auto whitespace-pre">${i(We(o))}</pre>
    </details>
    <div class="flex gap-2 flex-wrap">
      <button class="btn-primary" data-scan-save="reconcile" ${s===0?`disabled`:``}>Save &amp; reconcile</button>
      <button class="btn-secondary" data-scan-save="save" ${s===0?`disabled`:``}>Save only</button>
    </div>`}function We(e){let t=e.filter(e=>b[e.manifest?.metadata?.name??``]);return JSON.stringify(t.map(e=>e.manifest),null,2)}async function Ge(e){if(!y||ge)return;let n=document.getElementById(`scanPartitionName`)?.value?.trim()??``;if(!n){d(`Enter a target partition name.`,`error`);return}let r=(y.bundle?.intents??[]).filter(e=>b[e.manifest?.metadata?.name??``]);if(r.length===0){d(`Select at least one intent.`,`error`);return}ge=!0;try{let i=await Ke(n);await t(`/api/partitions/${encodeURIComponent(n)}/bundle`,{method:`PUT`,headers:{"Content-Type":`application/json`},body:JSON.stringify({partition:i?.partition?.manifest??y.bundle.partition,partitionExpectedVersionID:i?.partition?.manifestVersionID||void 0,intents:r,removeMissingIntents:!1})}),e&&await t(`/api/partitions/${encodeURIComponent(n)}/reconcile`,{method:`POST`}),d(i?`Bundle added to partition ${n}.`:`Bundle saved to partition ${n}.`,`success`),window.location.search=`?partition=${encodeURIComponent(n)}`}finally{ge=!1}}async function Ke(e){try{return await t(`/api/partitions/${encodeURIComponent(e)}`)}catch{return null}}var S=()=>void 0,qe=[],Je=[],Ye=[],Xe=!1,C=!1,Ze=``,w=new Map,T=new Map,Qe=``;function $e(e){S=e,document.getElementById(`pusherSaveButton`)?.addEventListener(`click`,()=>ut().catch(S)),document.getElementById(`repoSaveButton`)?.addEventListener(`click`,()=>dt().catch(S)),document.getElementById(`repoAssignSave`)?.addEventListener(`click`,()=>mt().catch(S)),document.getElementById(`repoAssignCancel`)?.addEventListener(`click`,pt),et(`repoPartitionPicker`,w),et(`repoAssignPicker`,T),document.getElementById(`pusherList`)?.addEventListener(`click`,e=>{let t=e.target.closest(`[data-pusher-delete]`);t&&ht(t.dataset.pusherDelete??``).catch(S)}),document.getElementById(`repoList`)?.addEventListener(`click`,e=>{let t=e.target.closest(`[data-repo-action]`);if(!t)return;let n=t.dataset.repoName??``;t.dataset.repoAction===`delete`&&gt(n).catch(S),t.dataset.repoAction===`ingest`&&_t(n).catch(S),t.dataset.repoAction===`assign`&&ft(n)})}function et(e,t){let n=document.getElementById(e);n&&(n.addEventListener(`change`,n=>{let r=n.target;if(!(r instanceof HTMLInputElement))return;let i=r.dataset.bindingCheck;i&&(r.checked?t.set(i,t.get(i)??{selector:``,path:``}):t.delete(i),at(e,t))}),n.addEventListener(`input`,e=>{let n=e.target;if(!(n instanceof HTMLInputElement))return;let r=n.dataset.bindingSelector??n.dataset.bindingPath;if(!r)return;let i=t.get(r)??{selector:``,path:``};n.dataset.bindingSelector&&(i.selector=n.value),n.dataset.bindingPath&&(i.path=n.value),t.set(r,i)}))}function tt(e={}){nt(e);let t=document.getElementById(`registryPanel`);!t||t.classList.contains(`hidden`)||(Xe?(st(),ct(),at(`repoPartitionPicker`,w),at(`repoAssignPicker`,T)):ot().catch(S))}function nt(e){let t=e.targetPartition?.trim()??``,n=w.size===0||w.size===1&&Ze!==``&&w.has(Ze);if(e.resetTarget){n&&(w.clear(),Ze=``);return}t&&n&&(w.clear(),w.set(t,{selector:``,path:``}),Ze=t)}function rt(e){return e.bindings&&e.bindings.length?e.bindings:e.partition?[{partition:e.partition,path:e.path}]:[]}function it(e){return[...e.entries()].map(([e,t])=>{let n={partition:e};return t.selector.trim()&&(n.selector=t.selector.trim()),t.path.trim()&&(n.path=t.path.trim()),n})}function at(e,t){let n=document.getElementById(e);if(!n)return;let r=[...new Set([...Ye,...t.keys()])].sort((e,t)=>e.localeCompare(t));if(r.length===0){n.innerHTML=`<p class="text-[12px] text-[#566778]">No partitions found. Create one first from Applications or Build.</p>`;return}n.innerHTML=r.map(e=>{let n=t.has(e),r=t.get(e)??{selector:``,path:``};return`
      <div class="px-2.5 py-1.5 rounded border ${n?`border-[#00ADE4]/50 bg-[#00ADE4]/[0.06]`:`border-white/[0.07] bg-[#0D1220]`}">
        <label class="flex items-center gap-2 cursor-pointer">
          <input type="checkbox" data-binding-check="${a(e)}" ${n?`checked`:``} class="accent-[#00ADE4]" />
          <span class="text-[12px] text-[#E5ECF4] truncate flex-1">${i(e)}</span>
          ${n?``:`<span class="text-[11px] text-[#566778]">not assigned</span>`}
        </label>
        ${n?`
        <div class="grid grid-cols-2 gap-2 mt-2 pl-6">
          <input class="search-input" data-binding-selector="${a(e)}" value="${a(r.selector)}" placeholder="Stack selector (empty = all)" />
          <input class="search-input" data-binding-path="${a(e)}" value="${a(r.path)}" placeholder="/partitions/${a(e)}/payloads/aws" />
        </div>`:``}
      </div>`}).join(``)}async function ot(){let[e,n,r]=await Promise.all([t(`/api/pushers`),t(`/api/repos`),t(`/api/overview`).catch(()=>null)]);qe=e.pushers??[],Je=n.repos??[],Ye=(r?.partitions??[]).map(e=>String(e?.name??``)).filter(Boolean),Xe=!0,st(),ct(),lt(),at(`repoPartitionPicker`,w),at(`repoAssignPicker`,T)}function st(){let e=document.getElementById(`pusherList`);if(e){if(qe.length===0){e.innerHTML=`<p class="text-[13px] text-[#566778]">No pushers registered yet.</p>`;return}e.innerHTML=qe.map(e=>`
      <div class="flex items-center justify-between gap-3 border border-[#1d2b3f] rounded px-3 py-2">
        <div class="min-w-0">
          <div class="text-sm text-[#dbe7f7] font-semibold">${i(e.name)}</div>
          <div class="text-[12px] text-[#7f93ad] truncate">${i(e.observedApiUrl)}${e.account?` · account ${i(e.account)}`:``}${e.region?` · ${i(e.region)}`:``}</div>
        </div>
        <button class="btn-ghost text-[12px]" data-pusher-delete="${a(e.name)}">Remove</button>
      </div>`).join(``)}}function ct(){let e=document.getElementById(`repoList`);if(e){if(Je.length===0){e.innerHTML=`<p class="text-[13px] text-[#566778]">No repositories registered yet.</p>`;return}e.innerHTML=Je.map(e=>{let t=rt(e),n=t.length?t.map(e=>`<span class="pill">${i(e.partition)}${e.selector?` <span class="text-[#566778]">· ${i(e.selector)}</span>`:``}</span>`).join(` `):`<span class="pill">no partitions</span>`,r=e.lastSyncError?`<div class="text-[12px] text-[#f87171] truncate">${i(e.lastSyncError)}</div>`:e.lastSyncedAt?`<div class="text-[12px] text-[#566778]">last ingest ${i(c(e.lastSyncedAt))}</div>`:``;return`
      <div class="border border-[#1d2b3f] rounded px-3 py-2">
        <div class="flex items-center justify-between gap-3">
          <div class="min-w-0">
            <div class="text-sm text-[#dbe7f7] font-semibold">${i(e.name)}</div>
            <div class="flex flex-wrap gap-1 mt-1">${n}</div>
            <div class="text-[12px] text-[#7f93ad] truncate mt-1">pusher ${i(e.pusher)} · ${i(e.source||e.path||`no source`)}</div>
            ${r}
          </div>
          <div class="flex items-center gap-2 shrink-0">
            <button class="btn-secondary text-[12px] px-2 py-0.5" data-repo-action="assign" data-repo-name="${a(e.name)}">Assign</button>
            <button class="btn-secondary text-[12px] px-2 py-0.5" data-repo-action="ingest" data-repo-name="${a(e.name)}">Ingest</button>
            <button class="btn-ghost text-[12px]" data-repo-action="delete" data-repo-name="${a(e.name)}">Remove</button>
          </div>
        </div>
      </div>`}).join(``)}}function lt(){let e=document.getElementById(`repoPusher`);if(!e)return;let t=e.value;e.innerHTML=qe.map(e=>`<option value="${a(e.name)}">${i(e.name)}</option>`).join(``),t&&(e.value=t)}async function ut(){if(C)return;let e=e=>document.getElementById(e)?.value?.trim()??``,n={name:e(`pusherName`),account:e(`pusherAccount`),region:e(`pusherRegion`),observedApiUrl:e(`pusherUrl`),tokenEnv:e(`pusherTokenEnv`),token:e(`pusherToken`),description:e(`pusherDescription`)};if(!n.observedApiUrl){d(`Pusher URL is required`,`error`);return}C=!0;try{await t(`/api/pushers`,{method:`POST`,headers:{"Content-Type":`application/json`},body:JSON.stringify(n)}),d(`Pusher ${n.name||n.account} registered`,`success`),[`pusherName`,`pusherUrl`,`pusherToken`,`pusherDescription`].forEach(e=>{let t=document.getElementById(e);t&&(t.value=``)}),await ot()}finally{C=!1}}async function dt(){if(C)return;let e=e=>document.getElementById(e)?.value?.trim()??``,n=e(`repoName`),r=document.getElementById(`repoPusher`)?.value??``,i=it(w);if(!r){d(`Register a pusher first`,`error`);return}if(!n){d(`Repository name is required`,`error`);return}if(i.length===0){d(`Assign at least one partition`,`error`);return}let a={name:n,pusher:r,bindings:i,source:e(`repoSource`),branch:e(`repoBranch`),assemblySubdir:e(`repoAssemblySubdir`)||`cdk.out`,description:e(`repoDescription`)};C=!0;try{await t(`/api/repos`,{method:`POST`,headers:{"Content-Type":`application/json`},body:JSON.stringify(a)}),d(`Repo ${n} registered`,`success`),[`repoName`,`repoSource`,`repoBranch`,`repoPath`,`repoDescription`].forEach(e=>{let t=document.getElementById(e);t&&(t.value=``)}),w.clear(),Ze=``,await ot()}finally{C=!1}}function ft(e){let t=Je.find(t=>t.name===e);if(!t)return;Qe=e,T.clear();for(let e of rt(t))T.set(e.partition,{selector:e.selector??``,path:e.path??``});document.getElementById(`repoAssignPanel`)?.classList.remove(`hidden`);let n=document.getElementById(`repoAssignName`);n&&(n.textContent=e),at(`repoAssignPicker`,T)}function pt(){Qe=``,T.clear(),document.getElementById(`repoAssignPanel`)?.classList.add(`hidden`)}async function mt(){if(C||!Qe)return;let e=it(T);if(e.length===0){d(`Assign at least one partition`,`error`);return}C=!0;try{await t(`/api/repos/${encodeURIComponent(Qe)}/bindings`,{method:`PUT`,headers:{"Content-Type":`application/json`},body:JSON.stringify({bindings:e})}),d(`Assignments updated for ${Qe}`,`success`),pt(),await ot()}finally{C=!1}}async function ht(e){e&&(await t(`/api/pushers/${encodeURIComponent(e)}`,{method:`DELETE`}),d(`Pusher ${e} removed`,`success`),await ot())}async function gt(e){e&&(await t(`/api/repos/${encodeURIComponent(e)}`,{method:`DELETE`}),Qe===e&&pt(),d(`Repo ${e} removed`,`success`),await ot())}async function _t(e){if(!(!e||C)){C=!0;try{d(`Ingested ${e}: ${((await t(`/api/repos/${encodeURIComponent(e)}/ingest`,{method:`POST`})).intents??[]).length} intent(s)`,`success`),await ot()}finally{C=!1}}}var vt=e=>console.error(e),yt=[],bt=!1;function xt(e){vt=e,document.getElementById(`unmanagedRefreshButton`)?.addEventListener(`click`,()=>Ct().catch(vt)),document.getElementById(`unmanagedList`)?.addEventListener(`click`,e=>{let t=e.target.closest(`[data-unmanaged-associate]`);t&&Tt(t.dataset.unmanagedAssociate??``).catch(vt)})}function St(e={}){Ct().catch(vt)}async function Ct(){if(bt)return;bt=!0;let e=document.getElementById(`unmanagedList`);e&&(e.innerHTML=`<div class="loading-state text-sm text-[#566778]">Loading stacks…</div>`);try{let e=await t(`/api/aws/unmanaged`);yt=Array.isArray(e?.stacks)?e.stacks:[],wt(Array.isArray(e?.warnings)?e.warnings:[])}finally{bt=!1}}function wt(e){let t=document.getElementById(`unmanagedList`);if(!t)return;let n=e.length?`<div class="text-[12px] text-amber-300 mb-2">${e.map(i).join(`<br>`)}</div>`:``;if(!yt.length){t.innerHTML=n+`<div class="text-[12px] text-[#566778]">No unmanaged stacks detected. Every live stack is covered by a repository-backed partition.</div>`;return}t.innerHTML=n+`<table class="data-table w-full text-left">
    <thead><tr>
      <th>State</th><th>Stack</th><th>Region</th><th>Pusher</th><th>Status</th><th>Drift</th><th>Partition</th><th></th>
    </tr></thead>
    <tbody>${yt.map(e=>{let t=e.state===`missing-repo`?`<span class="badge badge-attention">missing repo</span>`:`<span class="badge">unmanaged</span>`,n=e.partition?i(e.partition):`—`,r=e.driftStatus?i(e.driftStatus):`—`;return`<tr>
      <td>${t}</td>
      <td class="font-mono text-[12px]">${i(e.stackName??``)}</td>
      <td class="text-[12px]">${i(e.region??``)}</td>
      <td class="text-[12px]">${i(e.pusher??``)}</td>
      <td class="text-[12px]">${i(e.stackStatus??``)}</td>
      <td class="text-[12px]">${r}</td>
      <td class="text-[12px]">${n}</td>
      <td><button class="btn-secondary" data-unmanaged-associate="${i(e.stackName??``)}">Associate repo</button></td>
    </tr>`}).join(``)}</tbody>
  </table>`}async function Tt(e){let n=yt.find(t=>t.stackName===e);if(!n)return;let r=window.prompt(`Repository URL to associate with ${e}:`);if(!r)return;let i={repoUrl:r,pusher:n.pusher??``,account:n.account??``,region:n.region??``};n.partition&&(i.partition=n.partition);let a=await t(`/api/aws/unmanaged/${encodeURIComponent(e)}/associate`,{method:`POST`,headers:{"Content-Type":`application/json`},body:JSON.stringify(i)});if(a?.publishError){d(`Repo registered, but the MonoFS publish failed: ${a.publishError}`,`error`),await Ct();return}let o=a?.publish;if(o?.repository?.pullRequestUrl)d(`MR/PR ${o.repository.pullRequestCreated?`created`:`ready to open`}: ${o.repository.pullRequestUrl} (${o.repository.targetBranch??a?.branch??``})`,`success`);else if(o?.repository?.targetBranch)d(`Published review branch ${o.repository.targetBranch}`,`success`);else{let e=Object.keys(a?.files??{}).length;d(`Prepared ${a?.branch??`adoption branch`} (${e} file(s)). Publish the branch through MonoFS to open the MR.`,`success`)}await Ct()}var Et=262,Dt=320,Ot=64,kt=3,At=28,jt=46,Mt=18,Nt=26,Pt=12,Ft=/^[a-z][a-z0-9-]*$/,It=`guardian.builder.v1.`,Lt=`guardian.builder.templates.v1`,E={aws:[`CDKStack`,`Secret`,`Volume`,`Config`,`Compute`,`LoadBalancer`,`Database`,`SQLDatabase`,`ObjectStore`,`Network`,`Observability`],kubernetes:[`Volume`,`Config`,`Secret`,`Compute`,`ImageBuild`,`TraefikRoute`,`LoadBalancer`,`ObjectStore`,`Database`,`SQLDatabase`,`Observability`,`K8sResource`,`Network`],docker:[`Volume`,`Config`,`Network`,`Secret`,`Compute`,`ImageBuild`,`TraefikRoute`,`LoadBalancer`,`ObjectStore`,`Database`,`SQLDatabase`,`Observability`],local:[`Compute`,`Database`,`SQLDatabase`],other:[`Compute`,`Database`,`SQLDatabase`,`Volume`,`Config`,`Network`,`Secret`,`LoadBalancer`,`TraefikRoute`,`ObjectStore`,`Observability`,`ImageBuild`,`CDKStack`,`K8sResource`]},D={aws:`AWS`,kubernetes:`Kubernetes`,docker:`Docker`,local:`Local`,other:`Pusher`},Rt={Compute:`Set an image, then connect a Network, Volume, or Config.`,LoadBalancer:`Connect the services it should route to.`,TraefikRoute:`Connect the compute service to expose.`,Database:`Connect a volume and a network.`,SQLDatabase:`Connect a volume and a network.`,ObjectStore:`Connect a volume or set an endpoint.`,Observability:`Connect target compute assets; the AWS pusher injects the OTEL sidecar.`,Volume:`Set a size, then connect a service.`,Config:`Add inline content or data files.`,Network:`Connect services that share this network.`,Secret:`Set a secret reference or inline value.`,ImageBuild:`Set the repository and source image.`,CDKStack:`Set the AWS payload path.`,K8sResource:`Set the manifest payload path.`},zt={Compute:[`image`,`replicas`,`ports`,`env`,`networks`],ImageBuild:[`repository`,`sourceImage`,`buildContext`,`dockerfile`],CDKStack:[`context`,`env`],Database:[`engine`,`version`,`database`,`user`,`port`,`volume`,`networks`],SQLDatabase:[`engine`,`version`,`database`,`user`,`port`,`volume`,`networks`],Volume:[`size`,`accessMode`,`class`],Config:[`format`,`content`,`data`],Network:[`driver`,`scope`,`internal`],Secret:[`secretRef`,`value`],LoadBalancer:[`targets`,`listeners`,`serviceType`,`config`,`networks`],TraefikRoute:[`hostname`,`target`,`portName`],ObjectStore:[`engine`,`endpoint`,`buckets`,`versioning`,`volume`,`config`,`networks`],Observability:[`provider`,`targets`,`endpoint`,`receivers`,`exporters`,`config`,`volume`,`networks`],K8sResource:[`namespace`]},O,k=null,Bt=[],Vt=new Map,A=[],Ht=!1,Ut=!1,Wt=new Map,Gt=[],Kt,qt,Jt,j=[],Yt=[],Xt=!1,M=null,Zt=null,Qt=new Map,$t=null,en=``,N=[],tn=!1;function nn(e){return O=e,tn||(tn=!0,window.addEventListener(`beforeunload`,e=>{k?.dirty&&(e.preventDefault(),e.returnValue=``)}),vi(),zr(),rn().catch(e.onError)),{render:()=>P(),open:e=>an(e)}}async function rn(){if(!(Ht||Ut)){Ut=!0;try{let[e,n]=await Promise.all([t(`/api/catalog`),t(`/api/pushers`).catch(()=>({pushers:[]}))]);Bt=e?.assetTypes??[],Vt=new Map(Bt.map(e=>[e.type,e]));let r=(e?.pushers??[]).map(e=>({name:e,kind:z(e)})),i=(n?.pushers??[]).map(e=>({name:e.name,kind:z(e.name),account:e.account,region:e.region,description:e.description})),a=new Map;for(let e of[...r,...i])a.set(e.name,{...a.get(e.name),...e});A=[...a.values()].sort((e,t)=>e.name.localeCompare(t.name))}finally{Ht=!0,Ut=!1}}}function an(e){if(!e){if(k&&k.isNew){P();return}if(k?.dirty){di(`Discard unsaved changes?`,`You have unsaved changes for "${k.editingPartition}". Starting a new partition will discard them.`,`Discard and start new`,!0).then(e=>{e&&on()}).catch(O.onError);return}on();return}if(k&&k.editingPartition===e){P();return}if(k?.dirty){di(`Discard unsaved changes?`,`You have unsaved changes for "${k.editingPartition||`new partition`}". Loading "${e}" will discard them.`,`Discard and load`,!0).then(t=>{t&&sn(e).catch(O.onError)}).catch(O.onError);return}sn(e).catch(O.onError)}function on(){k=cn(),j=[],Yt=[],hn(``),P()}function P(){if(!document.getElementById(`builderPanel`))return;let e=O.getSelectedPartition();if(!k){if(e){sn(e).catch(O.onError);return}Fn();return}if(e&&k.editingPartition&&e!==k.editingPartition&&!k.isNew){if(!k.dirty){sn(e).catch(O.onError);return}en!==e&&(en=e,di(`Unsaved changes`,`You are editing "${k.editingPartition}" with unsaved changes. Switch to "${e}"?`,`Switch`,!0).then(t=>{t&&(k=null,sn(e).catch(O.onError))}).catch(O.onError))}if(!Ht){In(),rn().then(()=>P()).catch(O.onError);return}Jt!==void 0&&(window.clearTimeout(Jt),Jt=void 0),zn(),Jn(),Xn(),yr(),si()}async function sn(e){await rn();let n=await t(`/api/partitions/${encodeURIComponent(e)}`);$t=n?.assignments??null,k=ln(e,n),j=[],Yt=[],Gt=[],Qt.clear(),hn(e),P()}function cn(){return $t=null,{editingPartition:``,isNew:!0,partitionVersionID:``,partition:{apiVersion:`guardian/v1alpha1`,kind:`Partition`,metadata:{name:``},spec:{deletionPolicy:`orphan`,reconciliation:{mode:`auto`,interval:`15m`}}},intents:[],selection:{kind:`partition`},dirty:!1}}function ln(e,t){let n=JSON.parse(JSON.stringify(t?.partition?.manifest??{}));n.apiVersion=n.apiVersion||`guardian/v1alpha1`,n.kind=n.kind||`Partition`,n.metadata=n.metadata??{name:e},n.metadata.name=n.metadata.name||e,n.spec=n.spec??{},n.spec.reconciliation=n.spec.reconciliation??{mode:`auto`,interval:`15m`};let r=(t?.intents??[]).map(e=>{let t=JSON.parse(JSON.stringify(e?.manifest??{})),n=t.spec??{},r=n.assets??[],i={uid:t.metadata?.name??e?.name??`intent`,name:t.metadata?.name??e?.name??`intent`,isNew:!1,dirty:!1,expectedVersionID:e?.manifestVersionID??``,intentType:n.intentType||`standard`,targetPusher:n.targetPusher||``,target:n.target??{},joins:[...n.joins??[]],locked:!!n.locked,hints:JSON.parse(JSON.stringify(n.hints??[])),assets:r,positions:{}};return bn(i),i});return{editingPartition:e,isNew:!1,partitionVersionID:t?.partition?.manifestVersionID??``,partition:n,intents:r,selection:r.length?{kind:`intent`,intentUid:r[0].uid}:{kind:`partition`},dirty:!1}}function un(e){return{apiVersion:`guardian/v1alpha1`,kind:`Intent`,metadata:{name:e.name},spec:{intentType:e.intentType||`standard`,joins:e.joins.length?[...e.joins]:void 0,targetPusher:e.targetPusher,target:fn(e.target),locked:e.locked,hints:e.hints?.length?e.hints:void 0,assets:e.assets.map(e=>dn(e))}}}function dn(e){let t={type:e.type,name:e.name,properties:e.properties??{}};return e.version&&(t.version=e.version),e.dependsOn?.length&&(t.dependsOn=[...e.dependsOn]),e.hints?.length&&(t.hints=e.hints),e.payload&&Object.keys(e.payload).length&&(t.payload=e.payload),e.skip&&(t.skip=!0),t}function fn(e){let t={};for(let n of[`cluster`,`namespace`,`region`,`account`]){let r=e?.[n];r&&String(r).trim()&&(t[n]=String(r).trim())}return Object.keys(t).length?t:void 0}function pn(){let e=k.intents.filter(e=>!e.isNew).map(e=>e.name),t=new Set(k.intents.map(e=>e.name)),n=e.filter(e=>!t.has(e)).length>0,r=k.intents.filter(e=>n||e.isNew||e.dirty).map(e=>({manifest:un(e),expectedVersionID:e.expectedVersionID||void 0}));return{partition:k.partition,partitionExpectedVersionID:k.partitionVersionID||void 0,intents:r,removeMissingIntents:n}}function F(e){k&&(k.dirty=!0,e&&(e.dirty=!0),Rn(),mn(),ai(),Jt!==void 0&&window.clearTimeout(Jt),Jt=window.setTimeout(()=>{Jt=void 0,Xn(),si()},500))}function mn(){k&&(qt!==void 0&&window.clearTimeout(qt),qt=window.setTimeout(()=>{try{let e=It+(k.editingPartition||`__new__`);localStorage.setItem(e,JSON.stringify({draft:k,savedAt:Date.now()}))}catch{}},400))}function hn(e){try{Xt=!!localStorage.getItem(It+(e||`__new__`))}catch{Xt=!1}}function gn(){if(k)try{let e=localStorage.getItem(It+(k.editingPartition||`__new__`));if(!e)return;let t=JSON.parse(e);if(!t?.draft)return;k=t.draft,Xt=!1,Gt=[],P(),O.onToast(`Draft restored`,`success`)}catch(e){O.onError(e)}}function _n(){if(!k)return;let e=k.editingPartition;di(`Discard changes?`,`This removes your unsaved edits and reloads the saved manifests.`,`Discard`,!0).then(t=>{if(t){try{localStorage.removeItem(It+(e||`__new__`))}catch{}Xt=!1,j=[],Yt=[],Gt=[],e?sn(e).catch(O.onError):(k=cn(),P())}}).catch(O.onError)}function I(){k&&(j.push(JSON.stringify(k)),j.length>60&&j.shift(),Yt=[])}function vn(){!k||!j.length||(Yt.push(JSON.stringify(k)),k=JSON.parse(j.pop()),P())}function yn(){!k||!Yt.length||(j.push(JSON.stringify(k)),k=JSON.parse(Yt.pop()),P())}function bn(e){let t=xn(e.assets),n=new Map(e.assets.map(e=>[e.name,e])),r=new Set(e.assets.map(e=>e.name)),i=e=>{let t=n.get(e);if(!t)return[];let i=new Set;for(let e of t.dependsOn??[])r.has(e)&&i.add(e);for(let n of L(t.type))for(let a of R(t,n))r.has(a)&&a!==e&&i.add(a);return[...i]},a=new Map,o=new Set,s=e=>{let t=a.get(e);if(t!==void 0)return t;if(o.has(e))return 0;o.add(e);let n=0;for(let t of i(e))n=Math.max(n,s(t)+1);return o.delete(e),a.set(e,n),n},c=new Map;for(let e of t){let t=s(e);c.has(t)||c.set(t,[]),c.get(t).push(e)}let l=0;for(let t of[...c.keys()].sort((e,t)=>e-t)){let r=c.get(t);for(let t=0;t<r.length;t+=kt){let i=70;for(let a of r.slice(t,t+kt))e.positions[a]={x:20+l*(Et+Ot),y:i},i+=tr(n.get(a))+At;l++}}}function xn(e){let t=new Set(e.map(e=>e.name)),n=new Map(e.map(e=>[e.name,(e.dependsOn??[]).filter(e=>t.has(e))])),r=[],i=new Set,a=new Set,o=e=>{if(!(a.has(e)||i.has(e))){i.add(e);for(let t of n.get(e)??[])o(t);i.delete(e),a.add(e),r.push(e)}};return e.forEach(e=>o(e.name)),r}function Sn(e){let t=e.assets.map(t=>({asset:t,position:e.positions[t.name]})).filter(e=>!!e.position);if(!t.length)return{x:20,y:70};let n=new Map;for(let e of t)n.set(e.position.x,(n.get(e.position.x)??0)+1);for(let e of[...n.keys()].sort((e,t)=>e-t))if(!((n.get(e)??0)>=kt))return{x:e,y:Math.max(...t.filter(t=>t.position.x===e).map(e=>e.position.y+tr(e.asset)))+At};return{x:Math.max(...t.map(e=>e.position.x+Et))+Ot,y:70}}function Cn(e){let t=Dt;for(let n of e.assets){let r=e.positions[n.name];r&&(t=Math.max(t,r.x+Et+24))}return Math.round(t)}function wn(){I(),k?.intents.forEach(e=>bn(e)),mn(),P()}function L(e){let t=Wt.get(e);if(t)return t;let n=Vt.get(e),r=[];for(let t of n?.fields??[])if(t.control===`asset-ref`||t.control===`asset-refs`){let n={key:t.path,path:t.path,label:t.title,refTypes:t.refTypes??[],many:t.control===`asset-refs`};e===`LoadBalancer`&&t.path===`targets`&&(n.special=`targets`),e===`TraefikRoute`&&t.path===`target`&&(n.special=`traefikTarget`),r.push(n)}return e===`Compute`&&(r.push({key:`volumeMounts`,path:`volumeMounts`,label:`Volume`,refTypes:[`Volume`],many:!0,special:`volumeMounts`}),r.push({key:`configMounts`,path:`configMounts`,label:`Config`,refTypes:[`Config`],many:!0,special:`configMounts`}),r.push({key:`databaseRefs`,path:`env`,label:`Database`,refTypes:[`Database`,`SQLDatabase`],many:!0,special:`envRef`,envKey:`DATABASE_URL`,envOutput:`url`}),r.push({key:`objectStoreRefs`,path:`env`,label:`Object storage`,refTypes:[`ObjectStore`],many:!0,special:`envRef`,envKey:`OBJECT_STORE_ENDPOINT`,envOutput:`endpoint`})),Wt.set(e,r),r}function R(e,t){let n=e.properties??{};if(t.special===`volumeMounts`||t.special===`configMounts`){let e=t.special;return B(n[e]).map(t=>String(t?.[e===`volumeMounts`?`volume`:`config`]??``)).filter(Boolean)}if(t.special===`envRef`){let e=t.refTypes.includes(`ObjectStore`)?[`endpoint`,`bucket`]:[t.envOutput||`url`],r=new Set;for(let t of Object.values(n.env??{})){let n=Tn.exec(String(t??``));if(!n)continue;let[i,a]=n[2].split(`.`);i&&e.includes(a)&&r.add(i)}return[...r]}let r=n[t.path];return t.many?B(r).map(e=>String(e)).filter(Boolean):r?[String(r)]:[]}var Tn=/\$\{intent\.([a-zA-Z0-9-]+)\.outputs\.([a-zA-Z0-9_.-]+)\}/;function En(e){let t=Tn.exec(e);return t?t[2].split(`.`)[0]??``:``}function Dn(e,t,n){if(typeof e==`string`){let r=`\${intent.${t}.outputs.`;return e.split(r).join(`\${intent.${n}.outputs.`)}if(Array.isArray(e))return e.map(e=>Dn(e,t,n));if(e&&typeof e==`object`){let r={};for(let[i,a]of Object.entries(e))r[i]=Dn(a,t,n);return r}return e}function On(e){return e.refTypes.length?e.refTypes.join(` or `):`asset`}function kn(e,t){return!e.refTypes.length||e.refTypes.includes(t)}function An(e,t,n,r,i){let a=t.properties??={};if(n.special===`volumeMounts`){let e=B(a.volumeMounts);e.some(e=>e?.volume===r.name)||e.push({volume:r.name,path:i?.path||`/mnt/${r.name}`,readOnly:!!i?.readOnly}),a.volumeMounts=e}else if(n.special===`configMounts`){let e=B(a.configMounts);e.some(e=>e?.config===r.name)||e.push({config:r.name,path:i?.path||`/etc/${r.name}`,readOnly:!!i?.readOnly}),a.configMounts=e}else if(n.special===`envRef`){let t=r.type===`ObjectStore`&&z(e.targetPusher)===`aws`?`bucket`:n.envOutput||`url`,i=r.type===`ObjectStore`&&z(e.targetPusher)===`aws`?`S3_BUCKET`:n.envKey||`VALUE`,o=`\${intent.${e.name}.outputs.${r.name}.${t}}`,s={...a.env??{}};Object.values(s).some(e=>En(String(e??``))===r.name)||(s[i]!==void 0&&(i=`${r.name.toUpperCase().replace(/[^A-Z0-9]+/g,`_`)}_${i}`),s[i]=o,a.env=s)}else if(n.many){let e=B(a[n.path]).map(e=>String(e));e.includes(r.name)||e.push(r.name),a[n.path]=e}else a[n.path]=r.name;if(n.special===`traefikTarget`&&i?.portName&&(a.portName=i.portName),n.special===`targets`){let e=B(a.listeners),t=B(r.properties?.ports),n=i?.portName?t.find(e=>String(e?.name)===i.portName):t[0],o=Number(n?.port??n?.containerPort??0);o>0&&!e.some(e=>Number(e?.port)===o)&&(e.push({name:n?.name||`http`,port:o,protocol:n?.protocol||`http`,description:`Route to ${r.name}`}),a.listeners=e)}Mn(t,r.name)}function jn(e,t,n){let r=e.properties??{};if(t.special===`volumeMounts`)r.volumeMounts=B(r.volumeMounts).filter(e=>e?.volume!==n);else if(t.special===`configMounts`)r.configMounts=B(r.configMounts).filter(e=>e?.config!==n);else if(t.special===`envRef`){let e={...r.env??{}};for(let[t,r]of Object.entries(e))En(String(r??``))===n&&delete e[t];Object.keys(e).length?r.env=e:delete r.env}else if(t.many){let e=B(r[t.path]).map(e=>String(e)).filter(e=>e!==n);e.length?r[t.path]=e:delete r[t.path]}else String(r[t.path]??``)===n&&(delete r[t.path],t.special===`traefikTarget`&&delete r.portName);Nn(e)}function Mn(e,t){let n=B(e.dependsOn).map(e=>String(e));n.includes(t)||n.push(t),e.dependsOn=n}function Nn(e){let t=L(e.type),n=new Set;for(let r of t)for(let t of R(e,r))n.add(t);let r=B(e.dependsOn).map(e=>String(e)).filter(e=>n.has(e));r.length?e.dependsOn=r:delete e.dependsOn}function Pn(e){return B(e.properties?.ports).map(e=>({name:String(e?.name??``),port:Number(e?.port??e?.containerPort??0)})).filter(e=>e.port>0)}function Fn(){Ln(`Select a partition`);let e=document.getElementById(`builderCanvasContent`);e&&(e.innerHTML=`
      <div class="builder-empty">
        <h3>Build a partition visually</h3>
        <p>Pick a partition from the sidebar to edit it, or start a new one. Drag assets from the palette onto intent lanes and connect them.</p>
        <button class="btn-primary" id="builderEmptyNew">New partition</button>
      </div>`,document.getElementById(`builderEmptyNew`)?.addEventListener(`click`,()=>an())),Jn();let t=document.getElementById(`builderInspector`);t&&(t.innerHTML=`<div class="builder-inspector-empty">Select a partition or create a new one.</div>`),si(),Rn()}function In(){let e=document.getElementById(`builderCanvasContent`);e&&(e.innerHTML=`<div class="builder-empty"><p>Loading catalog…</p></div>`)}function Ln(e){let t=document.getElementById(`builderPartitionLabel`);t&&(t.textContent=e)}function Rn(){let e=document.getElementById(`builderDirtyBadge`);e&&e.classList.toggle(`hidden`,!k?.dirty);let t=document.getElementById(`builderRestoreDraft`);t&&t.classList.toggle(`hidden`,!Xt);let n=document.getElementById(`builderSave`);n&&(n.disabled=!k);let r=document.getElementById(`builderDiscard`);r&&(r.disabled=!k);let i=document.getElementById(`builderStatusText`);i&&(k?.isNew?i.textContent=`New partition`:k?i.textContent=k.dirty?`Editing · unsaved changes`:`Editing saved manifests`:i.textContent=``)}function zn(){Ln(k?.isNew?k.partition.metadata.name||`New partition`:k?.editingPartition||`Partition`),Rn(),Hn(),Un()}var Bn=`guardian.builder.tutorial.hidden`,Vn=!1;function Hn(){let e=document.querySelector(`.builder-topbar-actions`);if(!e||document.getElementById(`builderTutorialHelp`))return;let t=document.createElement(`button`);t.id=`builderTutorialHelp`,t.type=`button`,t.className=`btn-ghost`,t.title=`Builder tutorial`,t.setAttribute(`aria-label`,`Builder tutorial`),t.textContent=`?`,t.addEventListener(`click`,Wn),e.prepend(t)}function Un(){if(!(Vn||!k)){Vn=!0;try{if(localStorage.getItem(Bn)===`1`)return}catch{}Wn()}}function Wn(){if(document.getElementById(`builderTutorialOverlay`))return;let e=document.createElement(`div`);e.id=`builderTutorialOverlay`,e.className=`builder-modal-overlay`,e.innerHTML=`
    <div class="builder-modal" role="dialog" aria-modal="true" aria-labelledby="builderTutorialTitle">
      <h3 id="builderTutorialTitle">Builder tutorial</h3>
      <p>From empty canvas to a saved partition in four steps.</p>
      <ol class="builder-tutorial-steps">
        <li><b>Add an intent</b> and set its target pusher and placement. The palette follows the pusher, and each type says who supports it.</li>
        <li><b>Drag or click assets</b> from the palette into the lane. Unsupported types are dimmed with the reason and the pusher kinds that do support them.</li>
        <li><b>Connect</b> consumers to providers: drag from a socket on a card, or open the inspector and use “+ Connect”. Volumes, Configs and Networks are providers.</li>
        <li><b>Fix the Problems strip</b> at the bottom, then <b>Save</b>. Open the inspector's Raw YAML when you need full control.</li>
      </ol>
      <p class="builder-muted">Reopen this from the <b>?</b> button in the toolbar any time.</p>
      <div class="builder-modal-actions">
        <button class="btn-ghost" id="builderTutorialHide">Don't show again</button>
        <button class="btn-primary" id="builderTutorialClose">Got it</button>
      </div>
    </div>`,document.body.appendChild(e);let t=()=>e.remove();document.getElementById(`builderTutorialClose`)?.addEventListener(`click`,t),document.getElementById(`builderTutorialHide`)?.addEventListener(`click`,()=>{try{localStorage.setItem(Bn,`1`)}catch{}t()}),e.addEventListener(`click`,n=>{n.target===e&&t()}),document.addEventListener(`keydown`,e=>{e.key===`Escape`&&t()},{once:!0})}function Gn(e){return e.category===`Network`&&(e.type===`LoadBalancer`||e.type===`TraefikRoute`)?`Load balancing`:``}function Kn(e,t,n){let r=t!==null&&!t.includes(e.type),o=r?Yn(e.type,n):e.description;return`
    <button class="builder-palette-item${r?` unsupported`:``}" ${r?`aria-disabled="true"`:`draggable="true"`} data-asset-type="${a(e.type)}" title="${i(o)}">
      <span class="builder-palette-icon">${i(e.icon||`•`)}</span>
      <span class="builder-palette-copy"><b>${i(e.title)}</b><small>${i(o)}</small></span>
      ${r?`<span class="builder-palette-badge">unsupported</span>`:``}
    </button>`}function qn(e){let t=`${e.assets.length} asset${e.assets.length===1?``:`s`}${e.description?` · ${e.description}`:``}`;return`
    <div class="builder-template-item" data-template-id="${a(e.id)}" title="${a(e.description||`Insert "${e.name}" as a new intent`)}">
      <span class="builder-palette-icon">📋</span>
      <span class="builder-palette-copy"><b>${i(e.name)}</b><small>${i(t)}</small></span>
      <button class="builder-template-delete" data-delete-template="${a(e.id)}" title="Delete template">×</button>
    </div>`}function Jn(){let e=document.getElementById(`builderPaletteList`);if(!e)return;let t=document.getElementById(`builderPaletteSearch`)?.value?.trim().toLowerCase()??``,n=Bt.filter(e=>!t||e.title.toLowerCase().includes(t)||e.type.toLowerCase().includes(t)||(e.category??``).toLowerCase().includes(t)),r=new Map;for(let e of n){let t=e.category||`Other`,n=Gn(e);r.has(t)||r.set(t,new Map);let i=r.get(t);i.has(n)||i.set(n,[]),i.get(n).push(e)}let a=_r()??k?.intents[0],o=a?z(a.targetPusher):``,s=o?E[o]??E.other:null,c=[],l=t?N.filter(e=>e.name.toLowerCase().includes(t)||e.description.toLowerCase().includes(t)):N;l.length&&c.push(`<div class="builder-palette-group"><div class="builder-palette-group-title">Templates</div>${l.map(qn).join(``)}</div>`);for(let[e,t]of r){c.push(`<div class="builder-palette-group"><div class="builder-palette-group-title">${i(e)}</div>`);for(let e of t.get(``)??[])c.push(Kn(e,s,o));for(let[e,n]of t)if(e){c.push(`<div class="builder-palette-subgroup"><div class="builder-palette-subgroup-title">${i(e)}</div>`);for(let e of n)c.push(Kn(e,s,o));c.push(`</div>`)}c.push(`</div>`)}c.length||c.push(`<div class="builder-palette-empty">No asset types match.</div>`),e.innerHTML=c.join(``),e.querySelectorAll(`.builder-palette-item`).forEach(e=>{if(e.classList.contains(`unsupported`)){e.addEventListener(`click`,()=>{let t=_r()??k?.intents[0];t&&O.onToast(Yn(e.dataset.assetType,z(t.targetPusher)),`info`)});return}e.addEventListener(`dragstart`,t=>{t.dataTransfer?.setData(`text/plain`,e.dataset.assetType),t.dataTransfer?.setData(`application/x-guardian-asset`,e.dataset.assetType),t.dataTransfer&&(t.dataTransfer.effectAllowed=`copy`)}),e.addEventListener(`click`,()=>{let t=_r()??k?.intents[0];if(!t){O.onToast(`Add an intent first, then drop assets onto it.`,`info`);return}Nr(t.uid,e.dataset.assetType)})}),e.querySelectorAll(`[data-template-id]`).forEach(e=>{e.addEventListener(`click`,t=>{t.target.closest(`[data-delete-template]`)||Wr(e.dataset.templateId)})}),e.querySelectorAll(`[data-delete-template]`).forEach(e=>{e.addEventListener(`click`,t=>{t.stopPropagation(),Ur(e.dataset.deleteTemplate)})})}function Yn(e,t){let n=D[t]??t??`this`,r=Object.keys(E).filter(t=>t!==`other`&&E[t].includes(e)).map(e=>D[e]??e);return r.length?`${e} is not supported by ${n} pushers yet. Available with: ${r.join(`, `)}.`:`${e} is not supported by ${n} pushers yet.`}function Xn(){let e=document.getElementById(`builderCanvasContent`);if(!e||!k)return;let t=document.getElementById(`builderCanvas`),n=t?.scrollLeft??0,r=t?.scrollTop??0,i=[];k.intents.forEach(e=>{i.push(Zn(e))}),i.push(`
    <button class="builder-add-lane" id="builderAddLane">
      <span>+</span>
      <b>New intent</b>
      <small>Group assets that deploy together to one pusher.</small>
    </button>`),e.innerHTML=`<svg id="builderEdgeLayer" class="builder-edge-layer" xmlns="http://www.w3.org/2000/svg"></svg>${i.join(``)}`,sr(),ir(),window.requestAnimationFrame(()=>{ir(),t&&(t.scrollLeft=n,t.scrollTop=r)})}function Zn(e){let t=z(e.targetPusher),n=e.targetPusher||`no pusher`,r=Er(e),o=ti(e),s=e.assets.map(n=>Qn(e,n,t)).join(``);return`
    <section class="builder-lane${k?.selection.kind===`intent`&&k.selection.intentUid===e.uid?` selected`:``}" data-intent-uid="${a(e.uid)}" style="width:${Cn(e)}px">
      <header class="builder-lane-header" data-intent-uid="${a(e.uid)}">
        <div class="builder-lane-title-row">
          <input class="builder-lane-name" data-intent-uid="${a(e.uid)}" value="${i(e.name)}" aria-label="Intent name">
          <span class="builder-kind-badge kind-${a(t)}">${i(D[t]??t)}</span>
        </div>
        <div class="builder-lane-sub">
          <button class="builder-pusher-button" data-intent-uid="${a(e.uid)}" data-action="pick-pusher" title="Assign this intent to a pusher">
            ${i(n)}${r?` · ${i(r)}`:``}
          </button>
          <span class="builder-lane-count">${e.assets.length} asset${e.assets.length===1?``:`s`}${o?` · ${o} incompatible`:``}</span>
        </div>
      </header>
      <div class="builder-lane-body" data-intent-uid="${a(e.uid)}" style="min-height:${er(e)}px">
        ${s}
      </div>
    </section>`}function Qn(e,t,n){let r=Vt.get(t.type),o=L(t.type),s=e.positions[t.name]??Sn(e),c=$r(e,t),l=E[n]?.includes(t.type)??!0,u=k?.selection.kind===`asset`&&k.selection.intentUid===e.uid&&k.selection.assetName===t.name,d=tr(t),f=t.type===`Compute`?B(t.properties?.ports).slice(0,3):[],p=o.map(n=>{let r=R(t,n),o=r.map(r=>`
      <span class="builder-socket-chip ${Qr(e,n,r)?``:`missing`}" data-intent-uid="${a(e.uid)}" data-asset="${a(t.name)}" data-connector="${a(n.key)}" data-ref="${a(r)}" title="Disconnect ${a(r)}">${i(r)} <b>×</b></span>`).join(``);return`
      <div class="builder-socket${r.length===0?` empty`:``}" data-intent-uid="${a(e.uid)}" data-asset="${a(t.name)}" data-connector="${a(n.key)}" title="${i(`${n.label}: connect to ${On(n)}`)}">
        <span class="builder-socket-dot"></span>
        <span class="builder-socket-label">${i(n.label)}</span>
        <span class="builder-socket-refs">${o||`<span class="builder-socket-placeholder">drag to ${i(On(n).toLowerCase())}</span>`}</span>
      </div>`}).join(``),m=f.map(e=>`
    <div class="builder-port-row"><span class="builder-port-dot"></span>${i(String(e?.name??`port`))}<span class="builder-port-number">:${i(String(e?.port??e?.containerPort??`?`))}</span></div>`).join(``),h=c[0]?.message??Rt[t.type]??r?.description??``,ee=`<div class="builder-node-hint${c.length?` error`:``}">${i(h)}</div>`,te=c.length||!l;return`
    <div class="builder-node${u?` selected`:``}${te?` has-warning`:``}" data-intent-uid="${a(e.uid)}" data-asset="${a(t.name)}" style="transform:translate(${s.x}px,${s.y}px);width:${Et}px;height:${d}px">
      <div class="builder-node-header">
        <span class="builder-node-icon">${i(r?.icon??`•`)}</span>
        <span class="builder-node-title">${i(t.name)}</span>
        ${te?`<span class="builder-node-warning" title="${a($n(t,n)||c[0]?.message||``)}">!</span>`:``}
        <span class="builder-node-type">${i(r?.title??t.type)}</span>
      </div>
      ${m?`<div class="builder-node-ports">${m}</div>`:``}
      <div class="builder-node-sockets">${p}</div>
      ${ee}
    </div>`}function $n(e,t){return E[t]?.includes(e.type)?``:`${e.type} is not supported by ${D[t]??t} pushers.`}function er(e){let t=260;for(let n of e.assets){let r=e.positions[n.name];r&&(t=Math.max(t,r.y+tr(n)+40))}return t}function tr(e){let t=e.type===`Compute`?Math.min(3,B(e.properties?.ports).length):0,n=L(e.type).length;return jt+t*Mt+n*Nt+20+Pt}function nr(e,t){let n=L(e.type).findIndex(e=>e.key===t),r=e.type===`Compute`?Math.min(3,B(e.properties?.ports).length):0;return jt+8+r*Mt+Math.max(0,n)*Nt+Nt/2}function rr(){let e=new Map,t=document.getElementById(`builderCanvasContent`);return t&&t.querySelectorAll(`.builder-lane`).forEach(t=>{let n=t.querySelector(`.builder-lane-body`);e.set(t.dataset.intentUid,{left:t.offsetLeft,top:t.offsetTop,bodyTop:n?.offsetTop??0,width:t.offsetWidth})}),e}function ir(){let e=document.getElementById(`builderEdgeLayer`),t=document.getElementById(`builderCanvasContent`);if(!e||!t||!k)return;let n=rr(),r=[];for(let e of k.intents){let t=n.get(e.uid);if(!t)continue;let i=n=>{let r=e.positions[n];return r?{x:t.left+r.x,y:t.bodyTop+r.y}:null};for(let t of e.assets){let n=i(t.name);if(n)for(let a of L(t.type))R(t,a).forEach((o,s)=>{let c=e.assets.find(e=>e.name===o),l=i(o);if(!l)return;let u=l.y+jt/2,d=l.x+Et,f=n.y+nr(t,a.key)+s*2,p=n.x,m=Math.max(50,Math.abs(p-d)/2),h=c&&kn(a,c.type)?``:` invalid`;r.push(`<path class="builder-edge${h}" d="M ${d} ${u} C ${d+m} ${u}, ${p-m} ${f}, ${p} ${f}" />`)})}}for(let e of k.intents){let t=n.get(e.uid);if(t)for(let i of e.joins){let e=k.intents.find(e=>e.name===i);if(!e)continue;let a=n.get(e.uid);if(!a)continue;let o=a.left+a.width,s=a.top+26,c=t.left,l=t.top+26,u=Math.max(40,Math.abs(c-o)/2);r.push(`<path class="builder-edge join" d="M ${o} ${s} C ${o+u} ${s}, ${c-u} ${l}, ${c} ${l}" />`)}}if(M){let e=or(M);e&&r.push(`<path class="builder-edge pending" data-pending="1" d="M ${e.x} ${e.y} L ${ar.x} ${ar.y}" />`)}e.setAttribute(`width`,String(t.scrollWidth)),e.setAttribute(`height`,String(t.scrollHeight)),e.innerHTML=r.join(``)}var ar={x:0,y:0};function or(e){let t=k?.intents.find(t=>t.uid===e.intentUid);if(!t)return null;let n=t.assets.find(t=>t.name===e.assetName);if(!n)return null;let r=t.positions[n.name],i=rr().get(t.uid);return!r||!i?null:{x:i.left+r.x+Et,y:i.bodyTop+r.y+nr(n,e.connector.key)}}function sr(){let e=document.getElementById(`builderCanvasContent`);e&&(e.querySelectorAll(`.builder-lane-name`).forEach(e=>{e.addEventListener(`change`,()=>Gr(e.dataset.intentUid,e.value)),e.addEventListener(`keydown`,t=>{t.key===`Enter`&&e.blur()})}),e.querySelectorAll(`[data-action="pick-pusher"]`).forEach(e=>{e.addEventListener(`click`,()=>Xr(e.dataset.intentUid))}),e.querySelectorAll(`.builder-lane`).forEach(e=>{e.addEventListener(`click`,t=>{t.target.closest(`button, input, select, textarea, .builder-node, .builder-socket`)||Mr(e.dataset.intentUid)})}),e.querySelectorAll(`.builder-node`).forEach(e=>{cr(e)}),e.querySelectorAll(`.builder-socket`).forEach(e=>{e.addEventListener(`pointerdown`,t=>{if(t.target.closest(`.builder-socket-chip`))return;t.preventDefault(),t.stopPropagation();let n=e.dataset.intentUid,r=e.dataset.asset,i=e.dataset.connector,a=k?.intents.find(e=>e.uid===n),o=a?.assets.find(e=>e.name===r),s=o?L(o.type).find(e=>e.key===i):void 0;!a||!o||!s||ur(n,r,s,t)})}),e.querySelectorAll(`.builder-socket-chip`).forEach(e=>{e.addEventListener(`click`,t=>{t.stopPropagation(),gr(e.dataset.intentUid,e.dataset.asset,e.dataset.connector,e.dataset.ref)})}),e.querySelectorAll(`.builder-lane-body`).forEach(e=>{e.addEventListener(`dragover`,t=>{t.preventDefault(),t.dataTransfer&&(t.dataTransfer.dropEffect=`copy`),e.classList.add(`drag-over`)}),e.addEventListener(`dragleave`,()=>e.classList.remove(`drag-over`)),e.addEventListener(`drop`,t=>{t.preventDefault(),e.classList.remove(`drag-over`);let n=t.dataTransfer?.getData(`application/x-guardian-asset`)||t.dataTransfer?.getData(`text/plain`);if(!n||!Vt.has(n))return;let r=e.getBoundingClientRect(),i={x:Math.max(8,t.clientX-r.left-Et/2),y:Math.max(8,t.clientY-r.top-jt/2)};Nr(e.dataset.intentUid,n,i)})}),document.getElementById(`builderAddLane`)?.addEventListener(`click`,()=>Rr()))}function cr(e){let t=e.dataset.intentUid,n=e.dataset.asset,r=!1,i=!1,a=0,o=0,s=0,c=0,l=null;e.addEventListener(`pointerdown`,l=>{let u=l.target;if(l.button!==0||u.closest(`input, button, .builder-socket, .builder-socket-chip`))return;r=!0,i=!1,a=l.clientX,o=l.clientY;let d=(k?.intents.find(e=>e.uid===t))?.positions[n]??{x:0,y:0};s=d.x,c=d.y,e.setPointerCapture(l.pointerId),l.stopPropagation()}),e.addEventListener(`pointermove`,u=>{if(!r)return;let d=u.clientX-a,f=u.clientY-o;if(!i&&Math.abs(d)<3&&Math.abs(f)<3)return;i=!0;let p=k?.intents.find(e=>e.uid===t);if(!p)return;let m={x:Math.max(6,s+d),y:Math.max(6,c+f)};p.positions[n]=m,e.style.transform=`translate(${m.x}px,${m.y}px)`,e.classList.add(`dragging`),ir();let h=lr(u.clientX,u.clientY,t);h!==l&&(l=h,document.querySelectorAll(`.builder-lane`).forEach(e=>e.classList.toggle(`drop-target`,e.dataset.intentUid===h))),u.stopPropagation()}),e.addEventListener(`pointerup`,a=>{if(r){if(r=!1,e.releasePointerCapture(a.pointerId),e.classList.remove(`dragging`),document.querySelectorAll(`.builder-lane`).forEach(e=>e.classList.remove(`drop-target`)),i){let e=lr(a.clientX,a.clientY,t);e&&e!==t?Lr(t,e,n):(mn(),jr(t,n))}else jr(t,n);a.stopPropagation()}}),e.addEventListener(`click`,e=>{if(!e.target.closest(`input, button, .builder-socket, .builder-socket-chip`)&&!i){if(Zt){e.stopPropagation(),mr(t,n);return}jr(t,n)}})}function lr(e,t,n){let r=document.elementsFromPoint(e,t);for(let e of r){let t=e.closest?.(`.builder-lane`);if(t&&t.dataset.intentUid!==n)return t.dataset.intentUid??null}return null}function ur(e,t,n,r){M={intentUid:e,assetName:t,connector:n},ar={x:0,y:0};let i=or(M);i&&(ar={x:i.x,y:i.y}),document.body.classList.add(`builder-connecting`),dr(e,t,n),ir();let a=e=>{let t=document.getElementById(`builderCanvasContent`);if(!t)return;let n=t.getBoundingClientRect();ar={x:e.clientX-n.left,y:e.clientY-n.top},ir()},o=e=>{window.removeEventListener(`pointermove`,a),window.removeEventListener(`pointerup`,o),pr(e)};window.addEventListener(`pointermove`,a),window.addEventListener(`pointerup`,o),r.stopPropagation()}function dr(e,t,n){document.querySelectorAll(`.builder-node`).forEach(r=>{let i=r.dataset.intentUid===e,a=(k?.intents.find(e=>e.uid===r.dataset.intentUid))?.assets.find(e=>e.name===r.dataset.asset),o=i&&r.dataset.asset!==t&&!!a&&kn(n,a.type);r.classList.toggle(`connect-target`,o),r.classList.toggle(`connect-invalid`,i&&!o&&r.dataset.asset!==t)})}function fr(){document.querySelectorAll(`.builder-node`).forEach(e=>e.classList.remove(`connect-target`,`connect-invalid`)),document.body.classList.remove(`builder-connecting`)}function pr(e){let t=M;if(M=null,fr(),!t)return;let n=document.elementFromPoint(e.clientX,e.clientY)?.closest(`.builder-node`);if(!n){P();return}hr(t.intentUid,t.assetName,t.connector,n.dataset.intentUid,n.dataset.asset)}function mr(e,t){let n=Zt;n&&(Zt=null,fr(),hr(n.intentUid,n.assetName,n.connector,e,t))}async function hr(e,t,n,r,i){if(e!==r){O.onToast(`Assets can only reference others in the same intent.`,`error`),P();return}let a=k?.intents.find(t=>t.uid===e),o=a?.assets.find(e=>e.name===t),s=a?.assets.find(e=>e.name===i);if(!a||!o||!s)return;if(o===s){O.onToast(`An asset cannot connect to itself.`,`error`),P();return}if(!kn(n,s.type)){O.onToast(`${n.label} needs a ${On(n)}.`,`error`),P();return}let c={};if(n.special===`targets`||n.special===`traefikTarget`){let e=Pn(s);if(e.length>1){let t=await ui({title:n.special===`targets`?`Route to which port?`:`Expose which port?`,description:`${s.name} exposes ${e.length} ports.`,fields:[{name:`portName`,label:`Port`,type:`select`,options:e.map(e=>({value:e.name,label:`${e.name} :${e.port}`}))}],actions:[{key:`cancel`,label:`Cancel`},{key:`ok`,label:`Connect`,primary:!0}]});if(!t||t.action!==`ok`){P();return}c.portName=String(t.values.portName??``)}else e.length===1&&(c.portName=e[0].name)}I(),An(a,o,n,s,c),F(a),P(),O.onToast(`Connected ${s.name} → ${o.name}`,`success`)}function gr(e,t,n,r){let i=k?.intents.find(t=>t.uid===e),a=i?.assets.find(e=>e.name===t),o=a?L(a.type).find(e=>e.key===n):void 0;!i||!a||!o||(I(),jn(a,o,r),F(i),P())}function _r(){if(k&&(k.selection.kind===`intent`||k.selection.kind===`asset`))return k.intents.find(e=>e.uid===k.selection.intentUid)}function vr(){let e=_r();if(!e||k?.selection.kind!==`asset`)return;let t=e.assets.find(e=>e.name===k.selection.assetName);return t?{intent:e,asset:t}:void 0}function yr(){let e=document.getElementById(`builderInspector`);if(!(!e||!k)){if(k.selection.kind===`asset`){let t=vr();if(t){Cr(e,t.intent,t.asset);return}}if(k.selection.kind===`intent`){let t=_r();if(t){Sr(e,t);return}}br(e)}}function br(e){let t=k.partition,n=k.isNew;e.innerHTML=`
    <div class="builder-inspector-scroll">
      <div class="builder-inspector-head">
        <span class="builder-inspector-icon">📦</span>
        <div>
          <div class="builder-inspector-title">${n?`New partition`:i(t.metadata.name)}</div>
          <div class="builder-inspector-sub">Partition settings</div>
        </div>
      </div>
      <section class="builder-inspector-section">
        <h4>Identity</h4>
        ${n?`
        <label class="builder-field"><span>Name</span>
          <input id="builderPartitionName" class="search-input" value="${i(t.metadata.name)}" placeholder="my-partition">
          <small>Lowercase letters, digits, and dashes.</small>
        </label>`:``}
      </section>
      <section class="builder-inspector-section">
        <h4>Lifecycle</h4>
        <label class="builder-field"><span>Deletion policy</span>
          <select id="builderDeletionPolicy" class="search-input">
            ${[`orphan`,`destroy`].map(e=>`<option value="${e}"${t.spec.deletionPolicy===e?` selected`:``}>${e}</option>`).join(``)}
          </select>
        </label>
        <label class="builder-field"><span>Reconciliation</span>
          <select id="builderReconcileMode" class="search-input">
            ${[`auto`,`manual`,`readonly`].map(e=>`<option value="${e}"${t.spec.reconciliation?.mode===e?` selected`:``}>${e}</option>`).join(``)}
          </select>
        </label>
        <label class="builder-field"><span>Interval</span>
          <input id="builderReconcileInterval" class="search-input" value="${i(t.spec.reconciliation?.interval??``)}" placeholder="15m">
        </label>
      </section>
      <section class="builder-inspector-section">
        <h4>Defaults</h4>
        <label class="builder-field"><span>Default pusher</span>
          <select id="builderDefaultPusher" class="search-input">${qr(t.spec.defaults?.targetPusher??``)}</select>
        </label>
      </section>
      <section class="builder-inspector-section">
        <h4>Assignment</h4>
        ${$t?xr($t):`
          <div class="builder-assignment-note">Repository assignment is managed in Connections. Save the partition first, then bind a repo.</div>
        `}
        <button id="builderOpenConnections" class="btn-secondary" style="width:100%;margin-top:6px;">Manage in Connections</button>
      </section>
    </div>`;let r=document.getElementById(`builderPartitionName`);r?.addEventListener(`change`,()=>{k.partition.metadata.name=r.value.trim(),k.dirty=!0,mn(),zn(),ai()}),_i(`builderDeletionPolicy`,e=>{k.partition.spec.deletionPolicy=e,k.dirty=!0}),_i(`builderReconcileMode`,e=>{k.partition.spec.reconciliation=k.partition.spec.reconciliation??{},k.partition.spec.reconciliation.mode=e,k.dirty=!0}),_i(`builderReconcileInterval`,e=>{k.partition.spec.reconciliation=k.partition.spec.reconciliation??{},k.partition.spec.reconciliation.interval=e,k.dirty=!0}),_i(`builderDefaultPusher`,e=>{k.partition.spec.defaults=k.partition.spec.defaults??{},k.partition.spec.defaults.targetPusher=e,k.dirty=!0})}function xr(e){let t=Array.isArray(e?.repos)?e.repos:[],n=Array.isArray(e?.pushers)?e.pushers:[],r=Array.isArray(e?.warnings)?e.warnings:[],a=t[0],o=n.map(e=>`${e.name}${e.registered===!1?` (unregistered)`:``}`).join(`, `)||`no target pusher`;return`
    <div class="builder-assignment-block">
      ${a?`
        <div class="builder-assignment-row"><span>Repo</span><code>${i(a.name??``)}</code></div>
        <div class="builder-assignment-row"><span>Repo path</span><code>${i(a.repoPath??``)}</code></div>
        <div class="builder-assignment-row"><span>MonoFS</span><code>${i(a.monofsPath??``)}</code></div>
      `:`<div class="builder-assignment-note">No repository bound yet.</div>`}
      <div class="builder-assignment-row"><span>Targets</span><code>${i(o)}</code></div>
      ${r.length?`<div class="builder-assignment-warning">${r.map(e=>i(e)).join(`<br>`)}</div>`:``}
    </div>`}function Sr(e,t){let n=z(t.targetPusher),r=ei(t);e.innerHTML=`
    <div class="builder-inspector-scroll">
      <div class="builder-inspector-head">
        <span class="builder-inspector-icon">🗂️</span>
        <div>
          <div class="builder-inspector-title">${i(t.name)}</div>
          <div class="builder-inspector-sub">Intent · ${i(D[n]??n)}</div>
        </div>
      </div>
      <section class="builder-inspector-section">
        <h4>Placement</h4>
        <label class="builder-field"><span>Target pusher</span>
          <div class="builder-inline">
            <select id="builderIntentPusher" class="search-input">${qr(t.targetPusher)}</select>
            <button class="btn-ghost" id="builderMigratePusher">Migrate…</button>
          </div>
        </label>
        ${wr(t)}
        ${r.length?`<div class="builder-inspector-warning">${r.length} asset${r.length===1?``:`s`} not supported by this pusher: ${i(r.map(e=>e.name).join(`, `))}</div>`:``}
      </section>
      <section class="builder-inspector-section">
        <h4>Behaviour</h4>
        <label class="builder-field"><span>Type</span>
          <select id="builderIntentType" class="search-input">
            <option value="standard"${t.intentType===`observed`?``:` selected`}>standard</option>
            <option value="observed"${t.intentType===`observed`?` selected`:``}>observed (AWS, report-only)</option>
          </select>
        </label>
        <label class="builder-field checkbox"><input type="checkbox" id="builderIntentLocked"${t.locked?` checked`:``}> <span>Locked (block automatic changes)</span></label>
      </section>
      <section class="builder-inspector-section">
        <h4>Apply after</h4>
        <div class="builder-chip-row">
          ${k.intents.filter(e=>e.uid!==t.uid).map(e=>`
            <label class="builder-chip checkbox"><input type="checkbox" data-join="${a(e.name)}"${t.joins.includes(e.name)?` checked`:``}> ${i(e.name)}</label>`).join(``)||`<span class="builder-muted">No other intents yet.</span>`}
        </div>
      </section>
      <section class="builder-inspector-section">
        <h4>Reuse</h4>
        <button class="btn-ghost" id="builderSaveTemplate">Save as template</button>
        <p class="builder-muted">Templates appear in the palette and can seed a new intent in any partition.</p>
      </section>
      <section class="builder-inspector-section">
        <button class="btn-ghost builder-danger" id="builderDeleteIntent">Delete intent</button>
      </section>
    </div>`;let o=document.getElementById(`builderIntentPusher`);o?.addEventListener(`change`,()=>Zr(t,o.value)),document.getElementById(`builderMigratePusher`)?.addEventListener(`click`,()=>Xr(t.uid)),Tr(t),_i(`builderIntentType`,e=>{t.intentType=e,F(t),P()});let s=document.getElementById(`builderIntentLocked`);s?.addEventListener(`change`,()=>{t.locked=s.checked,F(t)}),e.querySelectorAll(`[data-join]`).forEach(e=>{e.addEventListener(`change`,()=>{let n=e.dataset.join,r=new Set(t.joins);e.checked?r.add(n):r.delete(n),t.joins=[...r],F(t),P()})}),document.getElementById(`builderDeleteIntent`)?.addEventListener(`click`,()=>Kr(t.uid)),document.getElementById(`builderSaveTemplate`)?.addEventListener(`click`,()=>Hr(t))}function Cr(e,t,n){let r=Vt.get(n.type),o=L(n.type),s=r?.fields??[],c=new Set(zt[n.type]??[]),l=s.filter(e=>c.has(e.path)),u=s.filter(e=>!c.has(e.path)),d=$r(t,n),f=E[z(t.targetPusher)]?.includes(n.type)??!0,p=Dr(n.type);e.innerHTML=`
    <div class="builder-inspector-scroll">
      <div class="builder-inspector-head">
        <span class="builder-inspector-icon">${i(r?.icon??`•`)}</span>
        <div>
          <div class="builder-inspector-title">${i(n.name)}</div>
          <div class="builder-inspector-sub">${i(r?.title??n.type)}</div>
        </div>
      </div>
      ${f?``:`<div class="builder-inspector-warning">${i($n(n,z(t.targetPusher)))}</div>`}
      ${d.length?`<div class="builder-inspector-issues">${d.map(e=>`<div class="builder-inspector-issue ${e.severity}">${i(e.message)}</div>`).join(``)}</div>`:``}
      <section class="builder-inspector-section">
        <h4>Identity</h4>
        <label class="builder-field"><span>Name</span>
          <input id="builderAssetName" class="search-input" value="${i(n.name)}">
          <small>Referenced by name inside this intent.</small>
        </label>
      </section>
      <section class="builder-inspector-section">
        <h4>Connections</h4>
        ${o.length?o.map(e=>{let t=R(n,e);return`
            <div class="builder-connection">
              <div class="builder-connection-head">
                <span>${i(e.label)}</span>
                <button class="btn-ghost builder-mini" data-connect="${a(e.key)}">+ Connect</button>
              </div>
              <div class="builder-chip-row">
                ${t.map(t=>`<span class="builder-chip">${i(t)} <button data-disconnect="${a(e.key)}" data-ref="${a(t)}">×</button></span>`).join(``)||`<span class="builder-muted">Not connected</span>`}
              </div>
            </div>`}).join(``):`<div class="builder-muted">This asset type has no inbound connections.</div>`}
      </section>
      <section class="builder-inspector-section">
        <h4>Configuration</h4>
        ${l.map(e=>Or(n,e)).join(``)}
        ${u.length?`<details class="builder-advanced"><summary>Advanced</summary>${u.map(e=>Or(n,e)).join(``)}</details>`:``}
      </section>
      ${p.length?`
      <section class="builder-inspector-section">
        <h4>Payload</h4>
        ${p.map(e=>`
          <label class="builder-field"><span>${i(e)} path</span>
            <input class="search-input" data-payload="${a(e)}" value="${i(n.payload?.[e]??``)}" placeholder="/partitions/.../payloads/...">
          </label>`).join(``)}
      </section>`:``}
      <section class="builder-inspector-section">
        <h4>Apply order</h4>
        <div class="builder-chip-row">
          ${B(n.dependsOn).map(e=>`<span class="builder-chip">after ${i(String(e))} <button data-remove-dep="${a(String(e))}">×</button></span>`).join(``)||`<span class="builder-muted">Auto-managed by connections</span>`}
        </div>
      </section>
      <section class="builder-inspector-section">
        <label class="builder-field checkbox"><input type="checkbox" id="builderAssetSkip"${n.skip?` checked`:``}> <span>Skip this asset</span></label>
      </section>
      <section class="builder-inspector-section builder-inspector-actions">
        <button class="btn-ghost" id="builderDuplicateAsset">Duplicate</button>
        <button class="btn-ghost builder-danger" id="builderDeleteAsset">Delete</button>
      </section>
    </div>`;let m=document.getElementById(`builderAssetName`);m?.addEventListener(`change`,()=>Ir(t,n,m.value)),m?.addEventListener(`keydown`,e=>{e.key===`Enter`&&m.blur()}),e.querySelectorAll(`[data-connect]`).forEach(e=>{e.addEventListener(`click`,()=>yi(t.uid,n.name,e.dataset.connect))}),e.querySelectorAll(`[data-disconnect]`).forEach(e=>{e.addEventListener(`click`,()=>gr(t.uid,n.name,e.dataset.disconnect,e.dataset.ref))}),e.querySelectorAll(`[data-remove-dep]`).forEach(e=>{e.addEventListener(`click`,()=>{I(),n.dependsOn=B(n.dependsOn).map(String).filter(t=>t!==e.dataset.removeDep),F(t),P()})}),kr(t,n,s),Ar(t,n);let h=document.getElementById(`builderAssetSkip`);h?.addEventListener(`change`,()=>{n.skip=h.checked,F(t)}),document.getElementById(`builderDuplicateAsset`)?.addEventListener(`click`,()=>Pr(t.uid,n.name)),document.getElementById(`builderDeleteAsset`)?.addEventListener(`click`,()=>Fr(t.uid,n.name))}function wr(e){let t=z(e.targetPusher),n=[];return t===`aws`?n.push({key:`region`,label:`Region`,placeholder:`eu-west-1`},{key:`account`,label:`Account`,placeholder:`123456789012`}):t===`kubernetes`||t===`docker`?n.push({key:`cluster`,label:`Cluster`,placeholder:`prod`},{key:`namespace`,label:`Namespace`,placeholder:`default`}):n.push({key:`cluster`,label:`Cluster`,placeholder:`local`},{key:`namespace`,label:`Namespace`,placeholder:``},{key:`region`,label:`Region`,placeholder:``},{key:`account`,label:`Account`,placeholder:``}),n.map(t=>`
    <label class="builder-field"><span>${i(t.label)}</span>
      <input class="search-input" data-placement="${t.key}" value="${i(e.target[t.key]??``)}" placeholder="${i(t.placeholder)}">
    </label>`).join(``)}function Tr(e){document.querySelectorAll(`[data-placement]`).forEach(t=>{t.addEventListener(`change`,()=>{let n=t.dataset.placement,r=t.value.trim();r?e.target[n]=r:delete e.target[n],F(e)})})}function Er(e){return z(e.targetPusher)===`aws`?[e.target.account,e.target.region].filter(Boolean).join(` / `):[e.target.cluster,e.target.namespace].filter(Boolean).join(` / `)}function Dr(e){return e===`CDKStack`?[`aws`]:e===`K8sResource`?[`k8s`]:[]}function Or(e,t){let n=fi(e.properties,t.path),r=t.control||`text`,o=`<span>${i(t.title)}</span>`,s=t.description?`<small>${i(t.description)}</small>`:``;if(r===`asset-ref`||r===`asset-refs`)return`
      <div class="builder-field">
        ${o}
        <div class="builder-chip-row">
          ${B(n).map(String).filter(Boolean).map(e=>`<span class="builder-chip">${i(e)}</span>`).join(``)||`<span class="builder-muted">Not connected</span>`}
        </div>
        ${s}
      </div>`;if(r===`boolean`)return`<label class="builder-field checkbox"><input type="checkbox" data-field-path="${a(t.path)}"${n===!0?` checked`:``}> ${o}${s}</label>`;if(r===`select`){let e=n==null?``:String(n);return`
      <label class="builder-field">${o}
        <select class="search-input" data-field-path="${a(t.path)}">
          ${(t.options??[]).map(t=>`<option value="${a(t)}"${e===t?` selected`:``}>${i(t)}</option>`).join(``)}
        </select>${s}
      </label>`}if(r===`json`||r===`list`){let c=`${e.name}:${t.path}`,l;return l=r===`json`?Qt.get(c)??(n===void 0?``:JSON.stringify(n,null,2)):B(n).map(String).join(`, `),`<label class="builder-field">${o}${r===`json`?`<textarea class="builder-json" rows="4" data-json-path="${a(t.path)}" data-raw-key="${a(c)}" spellcheck="false">${i(l)}</textarea>`:`<input class="search-input" data-list-path="${a(t.path)}" value="${i(l)}">`}${s}</label>`}return r===`number`?`<label class="builder-field">${o}<input class="search-input" type="number" data-field-path="${a(t.path)}" value="${n==null?``:i(String(n))}">${s}</label>`:r===`textarea`?`<label class="builder-field">${o}<textarea class="builder-json" rows="3" data-field-path="${a(t.path)}">${i(n===void 0?``:String(n))}</textarea>${s}</label>`:`<label class="builder-field">${o}<input class="search-input" data-field-path="${a(t.path)}" value="${n==null?``:i(String(n))}" placeholder="${i(t.placeholder??``)}">${s}</label>`}function kr(e,t,n){document.querySelectorAll(`[data-field-path]`).forEach(r=>{let i=r.dataset.fieldPath,a=n.find(e=>e.path===i),o=r instanceof HTMLSelectElement||r.type===`checkbox`||a?.control===`textarea`?`change`:`input`;r.addEventListener(o,()=>{let n=t.properties??={},o=r;if(o instanceof HTMLInputElement&&o.type===`checkbox`)pi(n,i,o.checked);else if(a?.control===`number`){let e=Number(o.value);pi(n,i,o.value===``?void 0:Number.isFinite(e)?e:o.value)}else pi(n,i,o.value);F(e)})}),document.querySelectorAll(`[data-json-path]`).forEach(n=>{let r=n.dataset.jsonPath;n.addEventListener(`input`,()=>Qt.set(`${t.name}:${r}`,n.value)),n.addEventListener(`blur`,()=>{let i=n.value.trim();if(!i){Qt.delete(`${t.name}:${r}`),I(),delete t.properties?.[r],F(e),P();return}try{let n=JSON.parse(i);Qt.delete(`${t.name}:${r}`),I(),t.properties=t.properties??{},t.properties[r]=n,F(e),P()}catch{O.onToast(`Invalid JSON for ${r}`,`error`)}})}),document.querySelectorAll(`[data-list-path]`).forEach(n=>{let r=n.dataset.listPath;n.addEventListener(`change`,()=>{let i=n.value.split(`,`).map(e=>e.trim()).filter(Boolean);I(),t.properties=t.properties??{},i.length?t.properties[r]=i:delete t.properties[r],F(e),P()})})}function Ar(e,t){document.querySelectorAll(`[data-payload]`).forEach(n=>{n.addEventListener(`change`,()=>{let r=n.dataset.payload,i=n.value.trim();t.payload=t.payload??{},i?t.payload[r]=i:delete t.payload[r],Object.keys(t.payload).length||delete t.payload,F(e)})})}function jr(e,t,n=!0){k&&(k.selection={kind:`asset`,intentUid:e,assetName:t},n&&P())}function Mr(e,t=!0){k&&(k.selection.kind===`intent`&&k.selection.intentUid===e||(k.selection={kind:`intent`,intentUid:e},t&&P()))}function Nr(e,t,n){let r=k?.intents.find(t=>t.uid===e),i=Vt.get(t);if(!r||!i||!k)return;I();let a=hi(mi(i.title),r.assets.map(e=>e.name)),o={type:t,name:a,properties:JSON.parse(JSON.stringify(i.template??{}))};r.assets.push(o),r.positions[a]=n??Sn(r),k.selection={kind:`asset`,intentUid:e,assetName:a},F(r),P();let s=Rt[t];s&&O.onToast(s,`info`)}function Pr(e,t){let n=k?.intents.find(t=>t.uid===e),r=n?.assets.find(e=>e.name===t);if(!n||!r||!k)return;I();let i=JSON.parse(JSON.stringify(r));i.name=hi(`${r.name}-copy`,n.assets.map(e=>e.name));let a=n.positions[t]??Sn(n);n.assets.push(i),n.positions[i.name]={x:Math.max(6,a.x+20),y:a.y+30},k.selection={kind:`asset`,intentUid:e,assetName:i.name},F(n),P()}function Fr(e,t){let n=k?.intents.find(t=>t.uid===e),r=n?.assets.find(e=>e.name===t);if(!n||!r||!k)return;let i=n.assets.filter(e=>L(e.type).some(n=>R(e,n).includes(t)));di(`Delete asset`,i.length?`${t} is used by ${i.map(e=>e.name).join(`, `)}. Deleting it will remove those connections.`:`Delete ${t}?`,`Delete`,!0).then(r=>{if(r){I();for(let e of i)for(let n of L(e.type))jn(e,n,t);n.assets=n.assets.filter(e=>e.name!==t),delete n.positions[t];for(let e of n.assets)e.dependsOn&&(e.dependsOn=e.dependsOn.filter(e=>e!==t),e.dependsOn.length||delete e.dependsOn);k.selection={kind:`intent`,intentUid:e},F(n),P()}}).catch(O.onError)}function Ir(e,t,n){let r=n.trim();if(r===t.name)return;if(!Ft.test(r)){O.onToast(`Names must start with a letter and use lowercase letters, digits, and dashes.`,`error`),P();return}if(e.assets.some(e=>e.name===r)){O.onToast(`Asset "${r}" already exists in this intent.`,`error`),P();return}I();let i=t.name;t.name=r;for(let t of e.assets){for(let e of L(t.type)){let n=t.properties??{};if(e.special===`volumeMounts`)for(let e of B(n.volumeMounts))e?.volume===i&&(e.volume=r);else if(e.special===`configMounts`)for(let e of B(n.configMounts))e?.config===i&&(e.config=r);else if(e.many){let t=B(n[e.path]).map(e=>String(e)===i?r:e);t.length&&(n[e.path]=t)}else String(n[e.path]??``)===i&&(n[e.path]=r)}t.dependsOn&&=t.dependsOn.map(e=>e===i?r:e)}if(e.hints?.length){let t=`assets.${i}.`;e.hints=e.hints.map(e=>e?.path?.startsWith(t)?{...e,path:`assets.${r}.`+e.path.slice(t.length)}:e)}let a=e.positions[i];delete e.positions[i],e.positions[r]=a??Sn(e),k&&(k.selection={kind:`asset`,intentUid:e.uid,assetName:r}),F(e),P(),O.onToast(`Renamed ${i} → ${r}`,`success`)}function Lr(e,t,n){let r=k?.intents.find(t=>t.uid===e),i=k?.intents.find(e=>e.uid===t),a=r?.assets.find(e=>e.name===n);if(!r||!i||!a||!k)return;if(i.assets.some(e=>e.name===a.name)){O.onToast(`"${a.name}" already exists in ${i.name}. Rename it first.`,`error`),P();return}I();let o=a.name;r.assets=r.assets.filter(e=>e.name!==o),delete r.positions[o];let s=[];for(let e of r.assets)for(let t of L(e.type))R(e,t).includes(o)&&(jn(e,t,o),s.push(e.name));for(let e of L(a.type))for(let t of R(a,e))i.assets.some(e=>e.name===t)||(jn(a,e,t),s.push(t));i.assets.push(a),i.positions[o]=Sn(i),k&&(k.selection={kind:`asset`,intentUid:i.uid,assetName:o}),F(r),F(i),P();let c=z(i.targetPusher);E[c]?.includes(a.type)??!0?s.length?O.onToast(`Moved ${o}; removed ${s.length} cross-intent connection${s.length===1?``:`s`}.`,`info`):O.onToast(`Moved ${o} to ${i.name}`,`success`):O.onToast(`Moved ${o} to ${i.name}, but ${a.type} is not supported by ${D[c]??c} pushers.`,`error`)}function Rr(){if(!k)return;I();let e=hi(`intent`,k.intents.map(e=>e.name)),t=k.partition.spec.defaults?.targetPusher||k.intents[0]?.targetPusher||A[0]?.name||``,n={uid:e,name:e,isNew:!0,dirty:!0,expectedVersionID:``,intentType:`standard`,targetPusher:t,target:Jr(t),joins:[],locked:!1,hints:[],assets:[],positions:{}};k.intents.push(n),k.selection={kind:`intent`,intentUid:n.uid},F(n),P()}function zr(){try{let e=localStorage.getItem(Lt);if(!e){N=[];return}let t=JSON.parse(e),n=Array.isArray(t)?t:t?.templates;N=Array.isArray(n)?n.filter(e=>e?.id&&e?.name&&Array.isArray(e?.assets)):[]}catch{N=[]}}function Br(){try{localStorage.setItem(Lt,JSON.stringify({templates:N}))}catch{}}function Vr(){return`tpl-${Date.now().toString(36)}-${Math.random().toString(36).slice(2,7)}`}async function Hr(e){if(!e.assets.length){O.onToast(`Add at least one asset before saving a template.`,`error`);return}let t=await ui({title:`Save "${e.name}" as template`,description:`Captures assets, connections, and settings so you can drop it into any partition.`,fields:[{name:`name`,label:`Template name`,type:`text`,value:e.name},{name:`description`,label:`Description`,type:`text`,value:``}],actions:[{key:`cancel`,label:`Cancel`},{key:`ok`,label:`Save template`,primary:!0}]});if(!t||t.action!==`ok`)return;let n=String(t.values.name??``).trim()||e.name,r={id:Vr(),name:n,description:String(t.values.description??``).trim(),savedAt:Date.now(),intentType:e.intentType,targetPusher:e.targetPusher,target:JSON.parse(JSON.stringify(e.target??{})),locked:e.locked,hints:JSON.parse(JSON.stringify(e.hints??[])),assets:JSON.parse(JSON.stringify(e.assets)),sourceIntent:e.name};N.unshift(r),Br(),P(),O.onToast(`Template "${n}" saved`,`success`)}function Ur(e){let t=N.find(t=>t.id===e);t&&di(`Delete template`,`Delete template "${t.name}"?`,`Delete`,!0).then(n=>{n&&(N=N.filter(t=>t.id!==e),Br(),P(),O.onToast(`Template "${t.name}" deleted`,`success`))}).catch(O.onError)}function Wr(e){let t=N.find(t=>t.id===e);if(!t||(k||on(),!k))return;I();let n=hi(mi(t.name),k.intents.map(e=>e.name)),r=t.targetPusher||k.partition.spec.defaults?.targetPusher||k.intents[0]?.targetPusher||A[0]?.name||``,i=Object.keys(t.target??{}).length?JSON.parse(JSON.stringify(t.target)):Jr(r),a=JSON.parse(JSON.stringify(t.assets));t.sourceIntent&&t.sourceIntent!==n&&(a=a.map(e=>e.properties?{...e,properties:Dn(e.properties,t.sourceIntent,n)}:e));let o={uid:n,name:n,isNew:!0,dirty:!0,expectedVersionID:``,intentType:t.intentType||`standard`,targetPusher:r,target:i,joins:[],locked:!1,hints:JSON.parse(JSON.stringify(t.hints??[])),assets:a,positions:{}};bn(o),k.intents.push(o),k.selection={kind:`intent`,intentUid:o.uid},F(o),P(),O.onToast(`Intent "${n}" added from template "${t.name}"`,`success`)}function Gr(e,t){let n=k?.intents.find(t=>t.uid===e),r=t.trim();if(!n||r===n.name)return;if(!Ft.test(r)){O.onToast(`Intent names must start with a letter and use lowercase letters, digits, and dashes.`,`error`),P();return}if(k.intents.some(t=>t.uid!==e&&t.name===r)){O.onToast(`Intent "${r}" already exists.`,`error`),P();return}I();let i=n.name;n.name=r;for(let e of k.intents){e.joins=e.joins.map(e=>e===i?r:e);for(let t of e.assets)t.properties&&=Dn(t.properties,i,r)}F(n),P()}function Kr(e){let t=k?.intents.find(t=>t.uid===e);!t||!k||di(`Delete intent`,`Delete "${t.name}" and its ${t.assets.length} asset${t.assets.length===1?``:`s`}?`,`Delete`,!0).then(n=>{if(n){I(),k.intents=k.intents.filter(t=>t.uid!==e);for(let e of k.intents)e.joins=e.joins.filter(e=>e!==t.name);k.selection=k.intents.length?{kind:`intent`,intentUid:k.intents[0].uid}:{kind:`partition`},F(t),P()}}).catch(O.onError)}function z(e){let t=String(e??``).toLowerCase();return t.includes(`aws`)?`aws`:t.includes(`k8s`)||t.includes(`kubernetes`)?`kubernetes`:t.includes(`docker`)?`docker`:t.includes(`local`)?`local`:`other`}function qr(e){let t=new Set(A.map(e=>e.name));e&&t.add(e);let n=[...t].sort((e,t)=>e.localeCompare(t));return`<option value=""${e?``:` selected`}>— select a pusher —</option>`+n.map(t=>{let n=A.find(e=>e.name===t)?.kind??z(t);return`<option value="${a(t)}"${e===t?` selected`:``}>${i(t)} · ${i(D[n]??n)}</option>`}).join(``)}function Jr(e){let t=z(e),n=A.find(t=>t.name===e);return t===`aws`?{account:n?.account??``,region:n?.region??``}:t===`kubernetes`||t===`docker`?{cluster:Yr(e),namespace:``}:t===`local`?{cluster:`local`}:{}}function Yr(e){let t=/^(?:k8s|kubernetes|docker|local)[-_:](.+)$/i.exec(e);return t?t[1]:``}async function Xr(e){let t=k?.intents.find(t=>t.uid===e);if(!t)return;let n=A.map(e=>({value:e.name,label:`${e.name} · ${D[e.kind]??e.kind}${e.account?` · ${e.account}`:``}${e.region?` · ${e.region}`:``}${e.description?` · ${e.description}`:``}`}));if(!n.length){O.onToast(`No pushers registered. Add one under Pushers & Repos.`,`error`);return}let r=await ui({title:`Assign "${t.name}" to a pusher`,description:`Assets move with the intent. Placement defaults are filled from the pusher.`,fields:[{name:`pusher`,label:`Pusher`,type:`select`,options:n,value:t.targetPusher}],actions:[{key:`cancel`,label:`Cancel`},{key:`ok`,label:`Assign`,primary:!0}]});!r||r.action!==`ok`||Zr(t,String(r.values.pusher??``))}async function Zr(e,t){if(!k||!t||t===e.targetPusher)return;let n=z(t),r=A.find(e=>e.name===t),i={...e.target};n===`aws`?(i.account=i.account||r?.account||``,i.region=i.region||r?.region||``,delete i.cluster):n===`kubernetes`||n===`docker`?(i.cluster=i.cluster||Yr(t),delete i.region,delete i.account):n===`local`&&(i.cluster=i.cluster||`local`,delete i.region,delete i.account);let a=e.assets.filter(e=>!(E[n]?.includes(e.type)??!0)),o=!1;if(a.length){let e=await ui({title:`Move to ${D[n]??n}?`,description:`${a.length} asset${a.length===1?` is`:`s are`} not supported by "${t}". You can keep them (they will fail at apply) or remove them now.`,fields:[{name:`remove`,label:`Remove unsupported assets (${a.map(e=>e.name).join(`, `)})`,type:`checkbox`,value:!1}],actions:[{key:`cancel`,label:`Cancel`},{key:`ok`,label:`Move intent`,primary:!0}]});if(!e||e.action!==`ok`){P();return}o=e.values.remove===!0}if(I(),o){let t=new Set(a.map(e=>e.name));e.assets=e.assets.filter(e=>!t.has(e.name));for(let n of t)delete e.positions[n]}e.targetPusher=t,e.target=i,e.dirty=!0,F(e),P(),O.onToast(`"${e.name}" assigned to ${t}`,`success`)}function Qr(e,t,n){let r=e.assets.find(e=>e.name===n);return!!r&&kn(t,r.type)}function $r(e,t){let n=[],r=t.properties??{},i=(r,i)=>{r||n.push({severity:`error`,message:i,intentUid:e.uid,asset:t.name,source:`client`})};switch(t.type){case`Compute`:i(V(r.image),`Image is required.`),e.assets.filter(e=>e.type===`Observability`&&B(e.properties?.targets).includes(t.name)).length>1&&n.push({severity:`error`,message:`Multiple Observability assets target ${t.name}; only one sidecar per compute is supported.`,intentUid:e.uid,asset:t.name,source:`client`});break;case`Database`:case`SQLDatabase`:i(V(r.engine),`Engine is required.`);break;case`ObjectStore`:i(V(r.engine),`Engine is required.`);break;case`Observability`:i(V(r.config)||V(r.endpoint)||B(r.exporters).length>0,`Set a config, endpoint, or exporter.`),z(e.targetPusher)===`aws`&&!B(r.targets).length&&n.push({severity:`warning`,message:`AWS observability runs as a sidecar; connect at least one Compute target.`,intentUid:e.uid,asset:t.name,source:`client`});break;case`TraefikRoute`:i(V(r.hostname),`Hostname is required.`),i(V(r.target),`Connect a Compute target.`);break;case`Secret`:{let a=V(r.value),o=V(r.secretRef);a&&o?n.push({severity:`error`,message:`Set either value or secretRef, not both.`,intentUid:e.uid,asset:t.name,source:`client`}):i(a||o,`Set value or secretRef.`);break}case`ImageBuild`:i(V(r.repository),`Repository is required.`),i(V(r.buildContext)||V(r.sourceImage),`Set buildContext or sourceImage.`),V(r.buildContext)&&i(V(r.dockerfile),`Dockerfile is required with buildContext.`);break;case`CDKStack`:i(V(t.payload?.aws),`payload.aws path is required.`);break;case`K8sResource`:i(V(t.payload?.k8s)||V(t.payload?.kubernetes),`payload.k8s path is required.`);break;case`Config`:i(V(r.content)||Object.keys(r.data??{}).length>0||Object.keys(t.payload??{}).length>0,`Set content, data, or payload.`);break;case`LoadBalancer`:i(B(r.targets).length>0||V(r.existingName),`Connect at least one Compute target.`),i(B(r.listeners).length>0,`Add at least one listener.`);break}for(let r of L(t.type))for(let i of R(t,r)){let a=e.assets.find(e=>e.name===i);a?kn(r,a.type)||n.push({severity:`error`,message:`${r.label} must reference a ${On(r)}, not ${a.type}.`,intentUid:e.uid,asset:t.name,source:`client`}):n.push({severity:`error`,message:`${r.label} references unknown asset "${i}".`,intentUid:e.uid,asset:t.name,source:`client`})}return n}function ei(e){let t=E[z(e.targetPusher)]??E.other;return e.assets.filter(e=>!t.includes(e.type))}function ti(e){return ei(e).length}function ni(){let e=[];if(!k)return e;let t=k.partition.metadata.name??``;t?Ft.test(t)||e.push({severity:`error`,message:`Partition name must start with a letter and use lowercase letters, digits, and dashes.`,source:`client`}):e.push({severity:`error`,message:`Partition name is required.`,source:`client`}),k.intents.length||e.push({severity:`error`,message:`Add at least one intent.`,source:`client`});let n=new Set;for(let t of k.intents){Ft.test(t.name)||e.push({severity:`error`,message:`Intent name "${t.name}" must start with a letter and use lowercase letters, digits, and dashes.`,source:`client`}),n.has(t.name)&&e.push({severity:`error`,message:`Duplicate intent "${t.name}".`,source:`client`}),n.add(t.name),t.targetPusher||e.push({severity:`error`,message:`Assign a target pusher.`,intentUid:t.uid,intent:t.name,source:`client`});let r=z(t.targetPusher);r===`aws`?(V(t.target.region)||e.push({severity:`error`,message:`AWS placement needs a region.`,intentUid:t.uid,intent:t.name,source:`client`}),V(t.target.account)||e.push({severity:`error`,message:`AWS placement needs an account.`,intentUid:t.uid,intent:t.name,source:`client`})):r===`kubernetes`||r===`docker`?V(t.target.cluster)||e.push({severity:`error`,message:`${D[r]} placement needs a cluster.`,intentUid:t.uid,intent:t.name,source:`client`}):!V(t.target.cluster)&&!V(t.target.namespace)&&!V(t.target.region)&&!V(t.target.account)&&e.push({severity:`error`,message:`Set a target placement.`,intentUid:t.uid,intent:t.name,source:`client`}),t.intentType===`observed`&&r!==`aws`&&e.push({severity:`error`,message:`Observed intents require an AWS pusher.`,intentUid:t.uid,intent:t.name,source:`client`});for(let n of t.joins)k.intents.some(e=>e.name===n)||e.push({severity:`error`,message:`Waits for unknown intent "${n}".`,intentUid:t.uid,intent:t.name,source:`client`});let i=new Set;for(let n of t.assets)Ft.test(n.name)||e.push({severity:`error`,message:`Asset name "${n.name}" is invalid.`,intentUid:t.uid,intent:t.name,asset:n.name,source:`client`}),i.has(n.name)&&e.push({severity:`error`,message:`Duplicate asset "${n.name}".`,intentUid:t.uid,intent:t.name,asset:n.name,source:`client`}),i.add(n.name),e.push(...$r(t,n)),(E[r]??E.other).includes(n.type)||e.push({severity:`warning`,message:`${n.type} is not supported by ${D[r]??r} pushers.`,intentUid:t.uid,intent:t.name,asset:n.name,source:`client`})}return ri(k.intents)&&e.push({severity:`error`,message:`Intent apply order contains a cycle.`,source:`client`}),e}function ri(e){let t=new Map(e.map(e=>[e.name,e])),n=new Set,r=new Set,i=e=>{if(r.has(e))return!1;if(n.has(e))return!0;n.add(e);let a=t.get(e);for(let e of a?.joins??[])if(i(e))return!0;return n.delete(e),r.add(e),!1};return e.some(e=>i(e.name))}function ii(){return[...ni(),...Gt]}function ai(){Kt!==void 0&&window.clearTimeout(Kt),Kt=window.setTimeout(()=>{oi().catch(()=>{})},900)}async function oi(){if(!k)return;let e=k.partition.metadata.name||k.editingPartition;if(!e||!Ft.test(e))return;let n=pn();Gt=((await t(`/api/partitions/${encodeURIComponent(e)}/validate`,{method:`POST`,headers:{"Content-Type":`application/json`},body:JSON.stringify(n)}))?.errors??[]).map(e=>({severity:`error`,message:e.message,intent:e.intent,asset:e.asset,source:`server`})),si()}function si(){let e=document.getElementById(`builderProblems`);if(!e||!k)return;let t=ii();if(!t.length){e.classList.add(`hidden`),e.innerHTML=``;return}let n=t.filter(e=>e.severity===`error`).length,r=t.length-n;e.classList.remove(`hidden`),e.innerHTML=`
    <div class="builder-problems-head">
      <b>${n?`${n} problem${n===1?``:`s`}`:``}${n&&r?` · `:``}${r?`${r} warning${r===1?``:`s`}`:``}</b>
      <button class="builder-problems-close" id="builderProblemsClose" aria-label="Hide problems">×</button>
    </div>
    <div class="builder-problems-list">
      ${t.map((e,t)=>`
        <button class="builder-problem ${e.severity} ${e.source}" data-problem-index="${t}">
          <span class="builder-problem-dot"></span>
          <span class="builder-problem-text">${e.intent?`<b>${i(e.intent)}</b>${e.asset?` / ${i(e.asset)}`:``} — `:``}${i(e.message)}</span>
        </button>`).join(``)}
    </div>`,document.getElementById(`builderProblemsClose`)?.addEventListener(`click`,()=>e.classList.add(`hidden`)),e.querySelectorAll(`[data-problem-index]`).forEach(e=>{e.addEventListener(`click`,()=>ci(t[Number(e.dataset.problemIndex)]))})}function ci(e){if(k){if(e.intent){let t=k.intents.find(t=>t.name===e.intent);t&&(e.asset?k.selection={kind:`asset`,intentUid:t.uid,assetName:e.asset}:k.selection={kind:`intent`,intentUid:t.uid})}else k.selection={kind:`partition`};if(P(),e.asset){let t=document.querySelector(`.builder-node[data-asset="${gi(e.asset)}"]`);t?.classList.add(`attention`),setTimeout(()=>t?.classList.remove(`attention`),1600)}}}async function li(){if(!k)return;let e=ni().filter(e=>e.severity===`error`);if(e.length){O.onToast(`Fix ${e.length} problem${e.length===1?``:`s`} before saving.`,`error`),ci(e[0]);return}let n=k.partition.metadata.name.trim();if(!n){O.onToast(`Partition name is required.`,`error`);return}let r=document.getElementById(`builderSave`);r&&(r.disabled=!0);try{let e=pn(),r=await t(`/api/partitions/${encodeURIComponent(n)}/bundle`,{method:`PUT`,headers:{"Content-Type":`application/json`},body:JSON.stringify(e)});k.partitionVersionID=r?.partitionVersionID??k.partitionVersionID;let i=r?.intentVersionIDs??{};for(let e of k.intents)i[e.name]&&(e.expectedVersionID=i[e.name]),e.dirty=!1,e.isNew=!1;k.isNew=!1,k.dirty=!1,k.editingPartition=n;let a=r?.removedIntents??[];try{localStorage.removeItem(It+n),localStorage.removeItem(It+`__new__`)}catch{}Xt=!1,Gt=[],O.onOpenPartition(n),P(),O.onToast(`Saved ${k.intents.length} intent${k.intents.length===1?``:`s`}${a.length?`, removed ${a.length}`:``}`,`success`)}catch(e){O.onToast(e?.message??`Save failed`,`error`),Gt=[{severity:`error`,message:e?.message??`Save failed`,source:`server`}],si()}finally{r&&(r.disabled=!1)}}function ui(e){return new Promise(t=>{let n=document.createElement(`div`);n.className=`builder-modal-overlay`;let r=(e.fields??[]).map(e=>e.type===`select`?`
          <label class="builder-field"><span>${i(e.label)}</span>
            <select class="search-input" data-modal-field="${a(e.name)}">
              ${(e.options??[]).map(t=>`<option value="${a(t.value)}"${e.value===t.value?` selected`:``}>${i(t.label)}</option>`).join(``)}
            </select>
            ${e.description?`<small>${i(e.description)}</small>`:``}
          </label>`:e.type===`checkbox`?`<label class="builder-field checkbox"><input type="checkbox" data-modal-field="${a(e.name)}"${e.value===!0||e.value===`true`?` checked`:``}> <span>${i(e.label)}</span></label>`:`<label class="builder-field"><span>${i(e.label)}</span><input class="search-input" data-modal-field="${a(e.name)}" value="${i(String(e.value??``))}"></label>`).join(``);n.innerHTML=`
      <div class="builder-modal" role="dialog" aria-modal="true">
        <h3>${i(e.title)}</h3>
        ${e.description?`<p>${i(e.description)}</p>`:``}
        ${e.bodyHtml??``}
        ${r}
        <div class="builder-modal-actions">
          ${e.actions.map(e=>`<button class="btn-${e.primary?`primary`:e.danger?`danger`:`ghost`}" data-modal-action="${a(e.key)}">${i(e.label)}</button>`).join(``)}
        </div>
      </div>`;let o=e=>{n.remove(),document.removeEventListener(`keydown`,c),t(e)},s=()=>{let e={};return n.querySelectorAll(`[data-modal-field]`).forEach(t=>{let n=t.dataset.modalField;t instanceof HTMLInputElement&&t.type===`checkbox`?e[n]=t.checked:e[n]=t.value}),e},c=e=>{e.key===`Escape`&&o(null)};n.querySelectorAll(`[data-modal-action]`).forEach(e=>{e.addEventListener(`click`,()=>o({action:e.dataset.modalAction,values:s()}))}),n.addEventListener(`click`,e=>{e.target===n&&o(null)}),document.addEventListener(`keydown`,c),document.body.appendChild(n),n.querySelector(`[data-modal-field]`)?.focus()})}async function di(e,t,n,r){return(await ui({title:e,description:t,actions:[{key:`cancel`,label:`Cancel`},{key:`ok`,label:n,primary:!r,danger:r}]}))?.action===`ok`}function B(e){return Array.isArray(e)?e:e==null||e===``?[]:[e]}function fi(e,t){return t.split(`.`).reduce((e,t)=>e?.[t],e)}function pi(e,t,n){let r=t.split(`.`),i=e;for(let e=0;e<r.length-1;e++)(typeof i[r[e]]!=`object`||i[r[e]]===null)&&(i[r[e]]={}),i=i[r[e]];n===void 0?delete i[r[r.length-1]]:i[r[r.length-1]]=n}function V(e){return e==null?``:String(e).trim()}function mi(e){return e.toLowerCase().replace(/[^a-z0-9]+/g,`-`).replace(/^-+|-+$/g,``).replace(/^[^a-z]+/,``)||`asset`}function hi(e,t){let n=e,r=2;for(;t.includes(n);)n=`${e}-${r++}`;return n}function gi(e){return e.replace(/["\\]/g,`\\$&`)}function _i(e,t){let n=document.getElementById(e);n&&n.addEventListener(`change`,()=>{t(n.value),mn(),zn(),ai()})}function vi(){document.getElementById(`builderSave`)?.addEventListener(`click`,()=>li().catch(O.onError)),document.getElementById(`builderOpenConnections`)?.addEventListener(`click`,()=>O.onOpenConnections()),document.getElementById(`builderInspector`)?.addEventListener(`click`,e=>{e.target.closest(`#builderOpenConnections`)&&O.onOpenConnections()}),document.getElementById(`builderAutoLayout`)?.addEventListener(`click`,()=>wn()),document.getElementById(`builderDiscard`)?.addEventListener(`click`,()=>_n()),document.getElementById(`builderRestoreDraft`)?.addEventListener(`click`,()=>gn()),document.getElementById(`builderPaletteSearch`)?.addEventListener(`input`,()=>Jn()),document.getElementById(`builderPartitionLabel`)?.addEventListener(`click`,()=>{k&&(k.selection={kind:`partition`},P())}),document.getElementById(`builderPanel`)?.addEventListener(`keydown`,e=>{let t=e,n=t.metaKey||t.ctrlKey;n&&t.key.toLowerCase()===`z`?(t.preventDefault(),t.shiftKey?yn():vn()):n&&t.key.toLowerCase()===`s`?(t.preventDefault(),li().catch(O.onError)):t.key===`Escape`&&(Zt||M)&&(Zt=null,M=null,fr(),P())})}function yi(e,t,n){let r=k?.intents.find(t=>t.uid===e),i=r?.assets.find(e=>e.name===t),a=i?L(i.type).find(e=>e.key===n):void 0;!r||!i||!a||(Zt={intentUid:e,assetName:t,connector:a},O.onToast(`Click a ${On(a)} to connect (Esc to cancel)`,`info`),dr(e,t,a))}var H,U=[],bi=[],xi=0,Si=!1,Ci=!1,W=null;function wi(e){H=e,document.getElementById(`newSetButton`)?.addEventListener(`click`,()=>Ui()),document.getElementById(`createPartitionButton`)?.addEventListener(`click`,()=>Ui()),document.getElementById(`manageSetsButton`)?.addEventListener(`click`,()=>qi()),document.getElementById(`partitionSearch`)?.addEventListener(`input`,Oi),document.getElementById(`setCards`)?.addEventListener(`click`,e=>{let t=e.target.closest(`[data-open-set]`);t&&H.onOpenSet(t.dataset.openSet??``)}),document.getElementById(`setPage`)?.addEventListener(`click`,e=>{let t=e.target.closest(`[data-set-action],[data-set-partition],[data-open-set]`);if(!t)return;if(t.dataset.openSet!==void 0){H.onOpenSet(t.dataset.openSet);return}if(t.dataset.setPartition){H.onSelectPartition(t.dataset.setPartition);return}let n=t.dataset.setAction;n===`add`&&Ji(H.getSelectedSet()),n===`new`&&Ui(),n===`edit`&&Ui(Ni()),n===`subset`&&Ui(void 0,H.getSelectedSet()),n===`delete`&&Ki(Ni()).catch(H.onError),n===`pin`&&Gi(Ni()).catch(H.onError),n===`connections`&&H.onOpenConnections()}),Di().catch(H.onError)}function Ti(){let e=H?.getSelectedSet()??``;return e?e===`ungrouped`?`Ungrouped`:U.find(t=>t.id===e)?.name??`Set`:`Sets`}function Ei(){Oi(),ki(),document.getElementById(`setPanel`)?.classList.contains(`active`)&&ji()}async function Di(){if(!Ci){Ci=!0;try{let[e,n]=await Promise.all([t(`/api/sets`),t(`/api/repos`).catch(()=>({repos:[]}))]);U=Array.isArray(e?.sets)?e.sets:[],xi=Number(e?.ungrouped??0),bi=Array.isArray(n?.repos)?n.repos:[],Si=!0,Ei(),H.onChanged?.()}finally{Ci=!1}}}function Oi(){let e=document.getElementById(`partitionList`);if(!e)return;if(!Si){e.className=`set-tree text-sm text-[#566778]`,e.textContent=`Loading sets…`;return}let t=document.getElementById(`partitionSearch`)?.value.trim().toLowerCase()??``,n=H.getSelectedSet(),r=H.getSelectedPartition(),o=new Map;for(let e of U){let t=e.parentId??``;o.has(t)||o.set(t,[]),o.get(t).push(e)}let s=U.filter(e=>!t||`${e.name} ${e.description??``} ${Object.values(e.labels??{}).join(` `)} ${(e.partitions??[]).join(` `)}`.toLowerCase().includes(t)),c=new Set(s.map(e=>e.id)),l=[],u=!n;l.push(`<button class="set-tree-row ${u?`active`:``}" data-open-set="">
    <span class="set-tree-chevron">▾</span><span class="set-tree-name">All sets</span><span class="set-tree-count">${U.length}</span>
    ${Bi(Ri())}
  </button>`);let d=(e,s)=>{if(!c.has(e.id)&&!o.get(e.id)?.some(e=>c.has(e.id)))return;let u=(o.get(e.id)??[]).sort((e,t)=>e.name.localeCompare(t.name)),f=n===e.id,p=(e.partitions??[]).includes(r);l.push(`<div class="set-tree-row ${f?`active`:``}" style="padding-left:${8+s*12}px" data-open-set="${a(e.id)}" role="button" tabindex="0">
      <span class="set-tree-chevron">${u.length?`▾`:`•`}</span>
      <span class="set-tree-name" title="${a(e.name)}">${i(e.name)}</span>
      <span class="set-tree-count">${Ii(e).length}</span>
      ${Bi(Li(e))}
    </div>`);for(let e of u)d(e,s+1);if(f||p||t){let n=(e.partitions??[]).filter(n=>!t||n.toLowerCase().includes(t)||e.name.toLowerCase().includes(t)),o=n.slice(0,40);for(let e of o){let t=Pi(e),n=e===r;l.push(`<div class="set-tree-row set-tree-partition ${n?`active`:``}" style="padding-left:${20+s*12}px" data-set-partition="${a(e)}" role="button" tabindex="0">
          <span class="status-dot status-dot-${a(t?.health??`neutral`)}"></span>
          <span class="set-tree-name">${i(e)}</span>
        </div>`)}n.length>o.length&&l.push(`<div class="set-tree-row set-tree-more" style="padding-left:${20+s*12}px" data-open-set="${a(e.id)}">+ ${n.length-o.length} more · view set</div>`)}};for(let e of(o.get(``)??[]).sort((e,t)=>Number(!!t.pinned)-Number(!!e.pinned)||e.name.localeCompare(t.name)))d(e,0);l.push(`<button class="set-tree-row ${n===`ungrouped`?`active`:``}" data-open-set="ungrouped">
    <span class="set-tree-chevron">•</span><span class="set-tree-name">Ungrouped</span><span class="set-tree-count">${xi}</span>
  </button>`),t&&!s.length&&xi===0&&l.push(`<div class="set-tree-empty">No sets or partitions match.</div>`),e.className=`set-tree`,e.innerHTML=l.join(``),e.querySelectorAll(`[data-open-set],[data-set-partition]`).forEach(e=>{e.addEventListener(`click`,e=>{let t=e.currentTarget;t.dataset.setPartition?H.onSelectPartition(t.dataset.setPartition):t.dataset.openSet!==void 0&&H.onOpenSet(t.dataset.openSet)}),e.addEventListener(`keydown`,e=>{(e.key===`Enter`||e.key===` `)&&(e.preventDefault(),e.currentTarget.click())})})}function ki(){let e=document.getElementById(`setCards`);if(!e)return;if(!Si){e.innerHTML=`<div class="empty-state">Loading sets…</div>`;return}let t=U.filter(e=>!e.parentId).sort((e,t)=>Number(!!t.pinned)-Number(!!e.pinned)||e.name.localeCompare(t.name)).map(e=>Ai(e));xi>0&&t.push(`<button class="set-card" data-open-set="ungrouped">
      <div class="set-card-head"><span class="name">Ungrouped</span><span class="right">Open →</span></div>
      <div class="set-card-desc">Partitions that are not in any named set.</div>
      <div class="set-card-stats"><strong>${xi} partitions</strong><span>·</span><span>ungrouped</span></div>
      <div class="set-card-foot"><span class="badge">Ungrouped</span><span class="action">View →</span></div>
    </button>`),e.innerHTML=t.length?t.join(``):`<div class="empty-state">No sets yet. Create one to group partitions.</div>`}function Ai(e){let t=Ii(e),n=zi(t),r=t.length,o=U.filter(t=>t.parentId===e.id),s=Object.entries(e.labels??{}).slice(0,4).map(([e,t])=>`<span class="chip"><span class="k">${i(e)}</span> ${i(t)}</span>`).join(``);return`<button class="set-card" data-open-set="${a(e.id)}">
    <div class="set-card-head"><span class="name">${i(e.name)}</span><span class="right">Open →</span></div>
    <div class="set-card-desc">${i(e.description||`No description yet.`)}</div>
    ${s?`<div class="set-card-labels">${s}</div>`:``}
    <div class="set-card-stats"><strong>${r} partition${r===1?``:`s`}</strong><span>·</span><span>${o.length} subset${o.length===1?``:`s`}</span></div>
    ${Bi(n,!0)}
    <div class="set-card-legend">${Vi(n)}</div>
    <div class="set-card-foot"><span class="badge">${o.length?`Nested set`:`Set`}</span><span class="action">View set →</span></div>
  </button>`}function ji(){let e=document.getElementById(`setPage`);if(!e)return;let t=H.getSelectedSet();if(!t){e.innerHTML=`<div class="card empty-state">Select a set from the sidebar.</div>`;return}let n=t===`ungrouped`?Mi():U.find(e=>e.id===t);if(!n){e.innerHTML=`<div class="card empty-state">Set not found.</div>`;return}let r=(n.partitions??[]).map(e=>({name:e,summary:Pi(e),repo:Fi(e)})),o=n.id===`ungrouped`?n.partitions??[]:Ii(n),s=zi(o),l=U.filter(e=>e.parentId===n.id),u=n.parentId?U.find(e=>e.id===n.parentId):void 0,d=Object.entries(n.labels??{}).map(([e,t])=>`<span class="chip"><span class="k">${i(e)}</span> ${i(t)}</span>`).join(``),f=r.slice(0,50),p=f.map(({name:e,summary:t,repo:n})=>{let r=(n?.bindings?.find(t=>t.partition===e)??(n?.partition===e?{partition:e,path:n.path}:void 0))?.path||`/partitions/${e}/payloads/aws`;return`<tr>
      <td><div class="app-name">${i(e)}</div><div class="repo-sub">${n?`primary set`:`no repository`}</div></td>
      <td><span class="status-dot status-dot-${a(t?.health??`neutral`)}"></span> <span class="status-text">${i(t?.displayStatus??t?.health??`Unknown`)}</span></td>
      <td class="source-cell">
        ${n?`<span class="mono">${i(n.name)}</span>`:`<span style="color:var(--ui-amber)">No repository</span>`}
        <div class="path-cell">${i(r)}</div>
      </td>
      <td>${n?`<span class="assign-ok">Assigned ✓</span>`:`<span class="assign-warn">Unassigned</span>`}</td>
      <td><button class="btn-secondary" style="padding:2px 8px;font-size:10px;" data-set-partition="${a(e)}">Open →</button></td>
    </tr>`}).join(``),m=l.map(e=>`<button class="subset-card" data-open-set="${a(e.id)}">
    <div class="name">${i(e.name)}</div>
    <div class="meta">${e.partitionCount??e.partitions?.length??0} partitions</div>
    <div class="foot"><span class="badge">${e.partitions?.length?`Nested set`:`empty`}</span><span style="margin-left:auto;color:var(--ui-accent);font-size:10px;">Open →</span></div>
  </button>`).join(``),h=o.map(e=>({name:e,repo:Fi(e)})),ee=h.filter(e=>e.repo).length,te=h.length-ee;e.innerHTML=`
    <div class="set-page">
      <section class="card set-hero">
        <div class="set-hero-top">
          <div>
            <p class="set-desc">${i(n.description||`No description yet.`)}</p>
            <div class="chip-row">
              ${u?`<span class="chip"><span class="k">parent</span> ${i(u.name)}</span>`:``}
              ${d}
            </div>
          </div>
          <div class="right">
            ${n.id===`ungrouped`?`
              <button class="btn-primary" data-set-action="new">Create a set</button>
            `:`
              <button class="btn-primary" data-set-action="add">Add partitions</button>
              <button class="btn-secondary" data-set-action="subset">New subset</button>
              <button class="btn-secondary" data-set-action="edit">Edit set</button>
              <button class="btn-ghost" data-set-action="pin">${n.pinned?`★ Pinned`:`☆ Pin`}</button>
            `}
          </div>
        </div>
        <div class="set-stats">
          <strong>${o.length} partition${o.length===1?``:`s`}</strong><span class="sep">·</span>
          <span><span class="status-dot status-dot-healthy"></span> ${s.healthy} healthy</span>
          <span><span class="status-dot status-dot-pending"></span> ${s.progressing} progressing</span>
          <span><span class="status-dot status-dot-attention"></span> ${s.attention} attention</span>
          <span class="right">${l.length} subsets · ${te} unassigned</span>
        </div>
      </section>
      <div class="set-page-layout">
        <div class="set-page-main">
          ${l.length?`<section class="card"><div class="card-header"><h2 class="card-title">Subsets</h2></div><div class="subset-grid">${m}</div></section>`:``}
          <section class="card">
            <div class="card-header"><div class="flex items-center gap-2"><h2 class="card-title">Partitions</h2><span class="pill">${r.length}</span></div><p class="muted">Only partitions in this set are listed here.</p></div>
            ${f.length?`<div class="set-table-wrap"><table class="table"><thead><tr><th>Application</th><th>Status</th><th>Source / MonoFS path</th><th>Assignment</th><th></th></tr></thead><tbody>${p}</tbody></table></div>`:`<div class="empty-state">${l.length?`No direct partitions. Open a subset to see its partitions.`:n.id===`ungrouped`?`No ungrouped partitions.`:`This set has no partitions yet. <button class="btn-secondary" data-set-action="add">Add partitions</button>`}</div>`}
            ${r.length>f.length?`<div class="set-table-more">Showing 1–${f.length} of ${r.length}. Use the set picker to search.</div>`:``}
          </section>
        </div>
        <div class="set-page-side">
          <section class="card">
            <div class="card-header"><h2 class="card-title">Health rollup</h2><p class="muted">Includes this set and child sets.</p></div>
            <div class="health-rollup-bar">${Bi(s,!0)}</div>
            <div class="rollup-legend">
              ${Hi(`Healthy`,s.healthy,`healthy`)}
              ${Hi(`Progressing`,s.progressing,`pending`)}
              ${Hi(`Observed`,s.observed,`neutral`)}
              ${Hi(`Attention`,s.attention,`attention`)}
              ${Hi(`Failing`,s.failing,`failing`)}
            </div>
            <div class="set-divider"></div>
            <div class="kv-list">
              <div class="kv-row"><span class="kv-label">Assigned</span><span class="kv-value">${ee} of ${o.length}</span></div>
              <div class="kv-row"><span class="kv-label">Unassigned</span><span class="kv-value" style="color:${te?`var(--ui-amber)`:`inherit`}">${te}</span></div>
              <div class="kv-row"><span class="kv-label">Sources</span><span class="kv-value">${[...new Set(h.map(e=>e.repo?.name).filter(Boolean))].join(`, `)||`—`}</span></div>
              <div class="kv-row"><span class="kv-label">Pushers</span><span class="kv-value">${[...new Set(h.map(e=>e.repo?.pusher).filter(Boolean))].join(`, `)||`—`}</span></div>
            </div>
            <button class="btn-secondary" style="margin-top:9px;" data-set-action="connections">Manage in Connections →</button>
          </section>
          <section class="card">
            <div class="card-header"><h2 class="card-title">Set details</h2><span class="pill">virtual</span></div>
            <div class="kv-list">
              <div class="kv-row"><span class="kv-label">Parent</span><span class="kv-value">${u?i(u.name):`—`}</span></div>
              <div class="kv-row"><span class="kv-label">Visibility</span><span class="kv-value">all users · contents filtered by ACL</span></div>
              <div class="kv-row"><span class="kv-label">Members</span><span class="kv-value">${o.length} partitions · ${l.length} subsets</span></div>
              <div class="kv-row"><span class="kv-label">Ownership</span><span class="kv-value">navigation only</span></div>
              <div class="kv-row"><span class="kv-label">Created</span><span class="kv-value">${n.createdAt?c(n.createdAt):`—`}</span></div>
              <div class="kv-row"><span class="kv-label">Updated</span><span class="kv-value">${n.updatedAt?c(n.updatedAt):`—`}</span></div>
            </div>
            <div class="desc" style="margin-top:8px;color:var(--ui-faint);font-size:10.5px;">Delete is disabled while the set contains partitions or subsets.</div>
            ${n.id===`ungrouped`?``:`<div style="display:flex;gap:5px;margin-top:8px;">
              <button class="btn-secondary" data-set-action="edit">Move / edit</button>
              <button class="btn-ghost" data-set-action="delete" ${r.length||l.length?`disabled`:``}>Delete</button>
            </div>`}
          </section>
        </div>
      </div>
    </div>`}function Mi(){let e=new Set;for(let t of U)for(let n of t.partitions??[])e.add(n);let t=(H.getOverview()?.partitions??[]).map(e=>String(e.name)).filter(t=>!e.has(t));return{id:`ungrouped`,name:`Ungrouped`,description:`Partitions that are not in any named set.`,partitions:t,partitionCount:t.length}}function Ni(){let e=H.getSelectedSet();return e===`ungrouped`?Mi():U.find(t=>t.id===e)}function Pi(e){return(H.getOverview()?.partitions??[]).find(t=>t.name===e)}function Fi(e){return bi.find(t=>(t.bindings??[]).some(t=>t.partition===e)||t.partition===e)}function Ii(e){let t=new Set(e.partitions??[]);for(let n of U.filter(t=>t.parentId===e.id))for(let e of Ii(n))t.add(e);return[...t].sort()}function Li(e){return zi(Ii(e))}function Ri(){return zi((H.getOverview()?.partitions??[]).map(e=>e.name))}function zi(e){let t={healthy:0,progressing:0,observed:0,attention:0,failing:0};for(let n of e){let e=Pi(n),r=String(e?.health??``).toLowerCase();r===`healthy`?t.healthy++:r===`failing`?t.failing++:r===`attention`?t.attention++:r===`progressing`||r===`pending`?t.progressing++:t.observed++}return t}function Bi(e,t=!1){if(!(e.healthy+e.progressing+e.observed+e.attention+e.failing))return`<span class="rollup-bar ${t?`large`:``} empty"></span>`;let n=(e,t)=>t?`<span class="seg ${e}" style="flex:${t}"></span>`:``;return`<span class="rollup-bar ${t?`large`:``}" title="${e.healthy} healthy · ${e.progressing} progressing · ${e.observed} observed · ${e.attention} attention · ${e.failing} failing">
    ${n(`healthy`,e.healthy)}${n(`progressing`,e.progressing)}${n(`neutral`,e.observed)}${n(`attention`,e.attention)}${n(`failing`,e.failing)}
  </span>`}function Vi(e){return[e.healthy?`<span class="item"><span class="status-dot status-dot-healthy"></span> ${e.healthy} healthy</span>`:``,e.progressing?`<span class="item"><span class="status-dot status-dot-pending"></span> ${e.progressing} progressing</span>`:``,e.observed?`<span class="item"><span class="status-dot status-dot-neutral"></span> ${e.observed} observed</span>`:``,e.attention?`<span class="item"><span class="status-dot status-dot-attention"></span> ${e.attention} attention</span>`:``,e.failing?`<span class="item"><span class="status-dot status-dot-failing"></span> ${e.failing} failing</span>`:``].filter(Boolean).join(``)}function Hi(e,t,n){return`<div class="rollup-legend-row"><span class="status-dot status-dot-${n}"></span><span class="label">${i(e)}</span><span class="value">${t}</span></div>`}function Ui(e,t){let n=!!(e&&e.id!==`ungrouped`),r=e?.parentId??t??``,o=U.filter(t=>t.id!==e?.id).map(e=>`<option value="${a(e.id)}"${e.id===r?` selected`:``}>${i(e.name)}</option>`).join(``);Xi(`<div class="set-modal-head"><h2>${n?`Edit set`:`New set`}</h2><button class="btn-ghost" data-modal-close>×</button></div>
    <div class="set-modal-body">
      <label class="field"><span>Name</span><input id="setModalName" class="input" value="${a(e?.name??``)}" placeholder="Payments"></label>
      <label class="field"><span>Description</span><textarea id="setModalDescription" class="textarea" rows="3" placeholder="Customer payment services for Department X in AWS production.">${i(e?.description??``)}</textarea></label>
      <label class="field"><span>Parent set</span><select id="setModalParent" class="input"><option value="">— no parent —</option>${o}</select></label>
      <label class="field"><span>Labels (key=value per line)</span><textarea id="setModalLabels" class="textarea" rows="3" placeholder="department=Department X&#10;cloud=aws&#10;env=prod">${Object.entries(e?.labels??{}).map(([e,t])=>`${e}=${t}`).join(`
`)}</textarea></label>
      <label class="set-modal-check"><input id="setModalPinned" type="checkbox" ${e?.pinned?`checked`:``}> Pin this set</label>
    </div>
    <div class="set-modal-actions"><button class="btn-ghost" data-modal-close>Cancel</button><button class="btn-primary" id="setModalSave">${n?`Save`:`Create set`}</button></div>`),document.getElementById(`setModalSave`)?.addEventListener(`click`,()=>Wi(e).catch(H.onError))}async function Wi(e){let n=document.getElementById(`setModalName`)?.value.trim()??``;if(!n){H.onToast(`Set name is required.`,`error`);return}let r=document.getElementById(`setModalDescription`)?.value.trim()??``,i=document.getElementById(`setModalParent`)?.value??``,a=document.getElementById(`setModalPinned`)?.checked??!1,o={};for(let e of(document.getElementById(`setModalLabels`)?.value??``).split(`
`)){let t=e.indexOf(`=`);t>0&&(o[e.slice(0,t).trim()]=e.slice(t+1).trim())}let s={name:n,description:r,parentId:i,labels:o,pinned:a};e&&e.id!==`ungrouped`?(await t(`/api/sets/${encodeURIComponent(e.id)}`,{method:`PATCH`,headers:{"Content-Type":`application/json`},body:JSON.stringify(s)}),H.onToast(`Set updated.`,`success`)):(await t(`/api/sets`,{method:`POST`,headers:{"Content-Type":`application/json`},body:JSON.stringify(s)}),H.onToast(`Set created.`,`success`)),Zi(),await Di()}async function Gi(e){!e||e.id===`ungrouped`||(await t(`/api/sets/${encodeURIComponent(e.id)}`,{method:`PATCH`,headers:{"Content-Type":`application/json`},body:JSON.stringify({...e,pinned:!e.pinned})}),await Di())}async function Ki(e){if(!(!e||e.id===`ungrouped`)&&window.confirm(`Delete set "${e.name}"?`))try{await t(`/api/sets/${encodeURIComponent(e.id)}`,{method:`DELETE`}),H.onToast(`Set deleted.`,`success`),Zi(),H.onOpenSet(``),await Di()}catch(e){H.onToast(e?.message?.includes(`not empty`)?`Move partitions and subsets first.`:e?.message??`Could not delete set.`,`error`)}}function qi(){Xi(`<div class="set-modal-head"><h2>Manage sets</h2><button class="btn-ghost" data-modal-close>×</button></div>
    <div class="set-modal-body"><div class="set-picker-list">${U.map(e=>{let t=e.parentId?U.find(t=>t.id===e.parentId):void 0;return`<div class="set-picker-row">
      <span class="set-manage-name">${i(e.name)}</span>
      <span class="pill">${e.partitionCount??e.partitions?.length??0} partitions${t?` · ${i(t.name)}`:``}</span>
      <button class="btn-secondary" style="padding:2px 7px;font-size:10px;" data-manage-edit="${a(e.id)}">Edit</button>
      <button class="btn-ghost" style="padding:2px 7px;font-size:10px;" data-manage-delete="${a(e.id)}">Delete</button>
    </div>`}).join(``)||`<div class="empty-state">No sets yet.</div>`}</div></div>
    <div class="set-modal-actions"><button class="btn-ghost" data-modal-close>Close</button><button class="btn-primary" id="setManageNew">New set</button></div>`),document.getElementById(`setManageNew`)?.addEventListener(`click`,()=>Ui()),W?.addEventListener(`click`,e=>{let t=e.target,n=t.closest(`[data-manage-edit]`);if(n){Ui(U.find(e=>e.id===n.dataset.manageEdit));return}let r=t.closest(`[data-manage-delete]`);if(r){let e=U.find(e=>e.id===r.dataset.manageDelete);e&&Ki(e).then(()=>{Zi(),qi()}).catch(H.onError)}})}function Ji(e){if(!e||e===`ungrouped`)return;let t=U.find(t=>t.id===e);if(!t)return;let n=(H.getOverview()?.partitions??[]).map(e=>String(e.name)).sort(),r=new Set(t.partitions??[]),o=n.map(e=>`<label class="set-picker-row"><input type="checkbox" value="${a(e)}" ${r.has(e)?`checked`:``}> <span>${i(e)}</span>${Fi(e)?`<span class="pill">${i(Fi(e).name)}</span>`:`<span class="pill">unassigned</span>`}</label>`).join(``);Xi(`<div class="set-modal-head"><h2>Add partitions to ${i(t.name)}</h2><button class="btn-ghost" data-modal-close>×</button></div>
    <div class="set-modal-body">
      <input id="setPickerSearch" class="input" placeholder="Search partitions…">
      <div class="set-picker-list" id="setPickerList">${o||`<div class="empty-state">No partitions available.</div>`}</div>
    </div>
    <div class="set-modal-actions"><button class="btn-ghost" data-modal-close>Cancel</button><button class="btn-primary" id="setPickerSave">Save</button></div>`),document.getElementById(`setPickerSearch`)?.addEventListener(`input`,e=>{let t=e.target.value.toLowerCase();document.querySelectorAll(`#setPickerList .set-picker-row`).forEach(e=>{e.style.display=e.textContent?.toLowerCase().includes(t)?``:`none`})}),document.getElementById(`setPickerSave`)?.addEventListener(`click`,()=>Yi(e,r).catch(H.onError))}async function Yi(e,n){let r=new Set;document.querySelectorAll(`#setPickerList input[type=checkbox]`).forEach(e=>{e.checked&&r.add(e.value)});let i=[...r].filter(e=>!n.has(e)),a=[...n].filter(e=>!r.has(e));await t(`/api/sets/${encodeURIComponent(e)}/partitions`,{method:`POST`,headers:{"Content-Type":`application/json`},body:JSON.stringify({add:i,remove:a})}),Zi(),H.onToast(`Set members updated.`,`success`),await Di()}function Xi(e){Zi();let t=document.getElementById(`setModalRoot`);t&&(W=document.createElement(`div`),W.className=`set-modal-backdrop`,W.innerHTML=`<div class="set-modal" role="dialog" aria-modal="true">${e}</div>`,t.appendChild(W),W.addEventListener(`click`,e=>{(e.target===W||e.target.closest(`[data-modal-close]`))&&Zi()}))}function Zi(){W?.remove(),W=null}var G=e(),Qi=nn({getSelectedPartition:()=>G.selectedPartition,onOpenPartition:e=>{ba(e).catch($)},onOpenConnections:()=>K(`registryPanel`),onToast:d,onError:$}),$i=`guardian.refreshIntervalMs`;try{let e=localStorage.getItem($i);if(e!==null){let t=Number(e);Number.isFinite(t)&&t>=1e3&&t<=12e4&&(G.refreshIntervalMs=t)}}catch{}var ea=1e3,ta=12e4;function na(){return G.refreshIntervalMs}function ra(){return Math.max(2e3,Math.floor(na()/5))}function ia(){return Math.min(ta,na()*3)}function aa(){return Math.min(ta,na()*3)}var oa=6e4,sa=6e4,ca=`guardian.theme`,la=[`midnight`,`graphite`,`ocean`,`forest`,`daylight`];function ua(){let e=document.getElementById(`themeSelect`),t=`midnight`;try{let e=localStorage.getItem(ca);e&&la.includes(e)&&(t=e)}catch{}da(t),e&&(e.value=t,e.addEventListener(`change`,()=>da(e.value)))}function da(e){let t=la.includes(e)?e:`midnight`;document.documentElement.setAttribute(`data-theme`,t);try{localStorage.setItem(ca,t)}catch{}let n=document.getElementById(`themeSelect`);n&&n.value!==t&&(n.value=t)}document.addEventListener(`DOMContentLoaded`,()=>{ua(),xa(),ds(),fa().catch($)});async function fa(){K(G.activePanel,G.selectedPartition?`existing`:void 0),await ya(),pa(),G.activePanel===`rolloutsPanel`&&G.selectedPartition&&_a()}function pa(){G.refreshTimer!==void 0&&window.clearTimeout(G.refreshTimer),G.refreshTimer=window.setTimeout(async()=>{try{await ya(G.activePanel!==`historyPanel`&&G.activePanel!==`rolloutsPanel`)}catch{}finally{pa()}},ma())}function ma(){return document.hidden?ia():Date.now()<G.fastRefreshUntil||ha()?ra():na()}function ha(){let e=G.detail;if(!e)return!1;if(String(e?.health?.status??``).toLowerCase()===`pending`)return!0;let t=Array.isArray(e?.intents)?e.intents:[];for(let e of t)switch(String(e?.status??``)){case`Checking`:case`Diffing`:case`Applying`:case`Destroying`:case`Ready`:case`Blocked`:return!0;default:break}return(Array.isArray(e?.health?.services)?e.health.services:[]).some(e=>e?.taskActive===!0)}function ga(e=oa){let t=Date.now()+e;t>G.fastRefreshUntil&&(G.fastRefreshUntil=t)}function _a(){G.rolloutsRefreshTimer!==void 0&&window.clearTimeout(G.rolloutsRefreshTimer),G.rolloutsRefreshTimer=window.setTimeout(async()=>{try{await Aa(!0)}catch{}finally{_a()}},aa())}function va(){G.rolloutsRefreshTimer!==void 0&&(window.clearTimeout(G.rolloutsRefreshTimer),G.rolloutsRefreshTimer=void 0)}async function ya(e=!0){r(`Refreshing…`),G.overview=await t(`/api/overview`),ja(),Ma();let n=G.selectedPartition,i=(G.overview?.partitions??[]).map(e=>e.name);n&&i.includes(n)&&e?await ba(n,!1):i.length||(G.selectedPartition=``,G.detail=null,G.history=null,G.rollouts=null,G.expandedRolloutKeys={},Fa(),q(),J(),Eo(),Sa()),G.selectedPartition||Fa(),r(`Updated just now`)}async function ba(e,n=!0){if(!e)return;ga();let r=G.selectedPartition===e;G.selectedPartition=e,G.activityDrawer={intentName:``,data:null,loading:!1,error:``},r||(G.expandedAssetKey=``,G.expandedRolloutKeys={},G.diagnosticDetails={},G.history=null,G.historyLoading=!1,G.historyError=``,G.rollouts=null,G.rolloutsLoading=!1,G.rolloutsError=``),Sa(),Ma(),G.detail=await t(`/api/partitions/${encodeURIComponent(e)}`),Fa(),q(),J(),Eo(),Y(),G.activePanel===`historyPanel`&&ka().catch($),G.activePanel===`rolloutsPanel`&&(Aa(r).catch($),_a()),G.activePanel===`builderPanel`&&!r&&Qi.render()}function K(e,t){G.activePanel=e,Sa(),document.querySelectorAll(`.panel`).forEach(t=>{let n=t.id===e;t.classList.toggle(`active`,n),t.classList.toggle(`hidden`,!n)}),document.querySelectorAll(`[data-tab-target]`).forEach(t=>{t.classList.toggle(`active`,t.dataset.tabTarget===e)});let n=[`topologyPanel`,`rolloutsPanel`,`historyPanel`,`setPanel`].includes(e)?`overviewPanel`:e===`scanPanel`?`unmanagedPanel`:e;document.querySelectorAll(`.nav-item[data-tab-target]`).forEach(e=>{e.classList.toggle(`active`,e.dataset.tabTarget===n)}),document.querySelectorAll(`[data-adopt-tab]`).forEach(t=>{t.classList.toggle(`active`,t.dataset.adoptTab===e)});let r=t===`existing`?G.selectedPartition:``,i=t===`new`;Ma(),Fa(),q(),J(),(e===`scanPanel`||e===`unmanagedPanel`)&&(ye({targetPartition:r,resetTarget:i}),St({targetPartition:r,resetTarget:i})),e===`registryPanel`&&tt({targetPartition:r,resetTarget:i}),e===`builderPanel`&&(t===`new`?Qi.open():t===`existing`&&G.selectedPartition?Qi.open(G.selectedPartition):Qi.render()),Y(),Ei(),e===`historyPanel`&&G.selectedPartition&&ka().catch($),e===`rolloutsPanel`&&G.selectedPartition&&(Aa(!0).catch($),_a()),e!==`rolloutsPanel`&&va()}function xa(){let e=new URLSearchParams(window.location.search),t=e.get(`partition`);t&&(G.selectedPartition=t.trim());let n=e.get(`set`);n&&(G.selectedSet=n.trim());let r=e.get(`panel`);[`overviewPanel`,`builderPanel`,`topologyPanel`,`rolloutsPanel`,`historyPanel`,`scanPanel`,`registryPanel`,`unmanagedPanel`,`setPanel`].includes(r??``)&&(G.activePanel=r);let i=Number.parseInt(e.get(`historyLimit`)??``,10);Number.isFinite(i)&&i>0&&(G.historyOptions.limit=i);let a=e.get(`historySince`);a&&(G.historyOptions.since=a);let o=e.get(`historyUntil`);o&&(G.historyOptions.until=o),Ta()}function Sa(){let e=new URLSearchParams(window.location.search);G.selectedPartition?e.set(`partition`,G.selectedPartition):e.delete(`partition`),G.activePanel&&G.activePanel!==`overviewPanel`?e.set(`panel`,G.activePanel):e.delete(`panel`),G.selectedSet?e.set(`set`,G.selectedSet):e.delete(`set`),G.historyOptions.limit===10?e.delete(`historyLimit`):e.set(`historyLimit`,String(G.historyOptions.limit)),G.historyOptions.since?e.set(`historySince`,G.historyOptions.since):e.delete(`historySince`),G.historyOptions.until?e.set(`historyUntil`,G.historyOptions.until):e.delete(`historyUntil`);let t=e.toString();window.history.replaceState(null,``,`${window.location.pathname}${t?`?${t}`:``}`)}async function Ca(e){let n=new URLSearchParams;return n.set(`limit`,String(G.historyOptions.limit)),G.historyOptions.since&&n.set(`since`,G.historyOptions.since),G.historyOptions.until&&n.set(`until`,G.historyOptions.until),t(`/api/partitions/${encodeURIComponent(e)}/history?${n.toString()}`)}async function wa(e){return t(`/api/partitions/${encodeURIComponent(e)}/rollouts`)}function Ta(){let e=document.getElementById(`historyLimit`);e&&(e.value=String(G.historyOptions.limit));let t=document.getElementById(`historySince`);t&&(t.value=l(G.historyOptions.since));let n=document.getElementById(`historyUntil`);n&&(n.value=l(G.historyOptions.until))}function Ea(){let e=document.getElementById(`historyLimit`),t=Number.parseInt(e?.value??``,10);G.historyOptions.limit=Number.isFinite(t)&&t>0?t:10;let n=document.getElementById(`historySince`);G.historyOptions.since=u(n?.value??``);let r=document.getElementById(`historyUntil`);G.historyOptions.until=u(r?.value??``)}async function Da(){if(Ea(),Sa(),!G.selectedPartition){q();return}await ka(!0)}async function Oa(){G.historyOptions={limit:10,since:``,until:``},Ta(),await Da()}async function ka(e=!1){if(!G.selectedPartition){G.history=null,G.historyLoading=!1,G.historyError=``,q(),Y();return}if(!G.historyLoading){if(!e&&G.history){q(),Y();return}G.historyLoading=!0,G.historyError=``,q(),Y();try{G.history=await Ca(G.selectedPartition)}catch(e){throw G.history=null,G.historyError=e?.message??`Failed to load history.`,e}finally{G.historyLoading=!1,q(),Y()}}}async function Aa(e=!1){if(!G.selectedPartition){G.rollouts=null,G.rolloutsLoading=!1,G.rolloutsError=``,J(),Y();return}if(!G.rolloutsLoading){if(!e&&G.rollouts){J(),Y();return}G.rolloutsLoading=!0,G.rolloutsError=``,J(),Y();try{G.rollouts=await wa(G.selectedPartition)}catch(e){throw G.rollouts=null,G.rolloutsError=e?.message??`Failed to load rollouts.`,e}finally{G.rolloutsLoading=!1,J(),Y()}}}function ja(){let e=G.overview?.summary??{};n(`summaryPartitions`,e.partitions??0),n(`summaryIntents`,e.intents??0),n(`summaryAssets`,e.assets??0),n(`summaryStable`,e.healthyAssets??e.servicesHealthy??0),n(`summaryAttention`,e.attentionAssets??e.servicesAttention??0),n(`summaryFailed`,e.failingAssets??e.failedIntents??0);let t=document.getElementById(`overviewHealthLine`);t&&(t.innerHTML=`<strong>${e.partitions??0} applications</strong>
      <span class="sep">·</span><span><span class="status-dot status-dot-healthy"></span> ${e.healthyAssets??0} healthy</span>
      <span><span class="status-dot status-dot-attention"></span> ${e.attentionAssets??0} attention</span>
      <span><span class="status-dot status-dot-failing"></span> ${e.failingAssets??0} failing</span>
      <span class="right">${e.assets??0} assets across ${e.intents??0} intents</span>`)}function Ma(){let e=G.activePanel===`overviewPanel`&&!G.selectedPartition;Ei(),e?Pa():Na()}function Na(){let e=document.getElementById(`appGrid`);e&&(e.className=`grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5`,e.innerHTML=``)}function Pa(){let e=document.getElementById(`appGrid`);if(!e)return;let t=(document.getElementById(`appGridSearch`)?.value??``).trim().toLowerCase(),n=(G.overview?.partitions??[]).filter(e=>t?`${e.name} ${Object.values(e.labels??{}).join(` `)}`.toLowerCase().includes(t):!0),r=n.slice(0,12);if(!r.length){e.className=`grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5 empty-state text-sm text-[#566778]`,e.textContent=G.overview?`No partitions match the filter.`:`Loading partitions…`;return}if(e.className=`grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5`,e.innerHTML=r.map(e=>{let t=e.name===G.selectedPartition,n=zo(e.labels??{});return`
      <button class="app-tile ${t?`active`:``}" data-partition="${a(e.name)}" data-health="${a(e.health??`neutral`)}">
        <div class="app-tile-body">
          <div class="app-tile-name">${i(e.name)}</div>
          ${e.originLabel?`<div class="app-tile-origin"><span class="origin-pill origin-${a(e.origin??`native`)}" title="${a(e.originDetail??``)}">${i(e.originLabel)}</span></div>`:``}
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
    `}).join(``),n.length>r.length){let t=document.createElement(`div`);t.className=`app-grid-hint`,t.textContent=`Showing ${r.length} of ${n.length} partitions. Use the set tree or search to narrow the list.`,e.appendChild(t)}e.querySelectorAll(`[data-partition]`).forEach(e=>{e.addEventListener(`click`,()=>ba(e.dataset.partition).catch($))})}function Fa(){let e=G.detail,t=document.getElementById(`heroContent`);if(Ia(),!t)return;if(G.activePanel!==`overviewPanel`){Y();return}if(!e){t.className=`loading-state text-sm text-[#566778]`,t.textContent=`Select a partition to inspect its current shape.`,Ba(),za(!1),Y();return}za(!0);let n=e.health??{},r={...e.partition?.manifest?.metadata?.labels??{},...e.partition?.manifest?.spec?.labels??{}};t.className=``,t.innerHTML=`
    <div class="hero-grid">
      <div class="hero-main">
        <div class="pill-row mb-2">
          ${X(n.status,n.displayStatus)}
          ${r.role?`<span class="pill">${i(r.role)}</span>`:``}
          ${r.component?`<span class="pill">${i(r.component)}</span>`:``}
          ${r.stack?`<span class="pill">${i(r.stack)}</span>`:``}
          <span class="pill">${i(e.partition.manifest.spec?.deletionPolicy??`orphan`)} deletion</span>
          <span class="pill">${i(e.partition.manifest.spec?.reconciliation?.mode??`manual`)} reconcile</span>
          ${e.compilerError?`<span class="badge badge-failing">Compiler warning</span>`:``}
        </div>
        <h2>${i(e.partition.manifest.metadata.name)}</h2>
        <p>${i(n.summary??`Partition summary unavailable.`)}</p>
        ${n.status===`pending`?Ro(e):``}
        <div class="pill-row mt-2">
          ${r.endpoint?`<span class="pill">${i(r.endpoint)}</span>`:``}
          ${r.topology?`<span class="pill">${i(r.topology)}</span>`:``}
          ${r.managedBy?`<span class="pill">${i(r.managedBy)}</span>`:``}
          ${(e.partition.state?.errors??[]).map(e=>`<span class="pill">${i(e)}</span>`).join(``)}
          ${e.compilerError?`<span class="pill">${i(e.compilerError)}</span>`:``}
        </div>
      </div>
      ${Ra(`Healthy`,n.healthy??0)}
      ${Ra(`Attention`,(n.attention??0)+(n.pending??0))}
      ${Ra(`Failing`,n.failing??0)}
    </div>
  `,Va(),Ka(),$a(),no(),Y()}function Ia(){let e=document.getElementById(`assignmentCard`),t=document.getElementById(`assignmentGrid`),n=document.getElementById(`assignmentBadge`);if(!e||!t)return;let r=G.detail;if(G.activePanel!==`overviewPanel`||!r||!r.assignments){e.classList.add(`hidden`);return}let o=r.assignments,s=Array.isArray(o.repos)?o.repos:[],l=Array.isArray(o.pushers)?o.pushers:[],u=Array.isArray(o.warnings)?o.warnings:[],d=l.some(e=>e.registered===!1),f=u.length>0||s.length===0||d;e.classList.remove(`hidden`),n&&(n.innerHTML=f?`<span class="assignment-warn">Needs attention</span>`:`<span class="assignment-ok">✓ Assignment complete</span>`);let p=s.length?s.map(e=>{let t=String(e.source??``).trim(),n=String(e.branch??`main`).trim()||`main`,r=String(e.commit??`unknown`).trim()||`unknown`,o=e.lastSyncedAt?c(e.lastSyncedAt):`never`,s=String(e.binding?.selector??``).trim();return`
      <div class="assignment-repo">
        <div class="assignment-repo-head">
          <strong>${i(e.name??`repository`)}</strong>
          ${t?`<a class="btn-secondary" style="padding:2px 8px;font-size:10px;" href="${a(t)}" target="_blank" rel="noreferrer">Open repo ↗</a>`:``}
        </div>
        <div class="assignment-meta">branch ${i(n)} · commit ${i(r)} · last ingest ${i(o)}</div>
        ${s?`<div class="assignment-meta">binding selector ${i(s)}</div>`:``}
        <div class="assignment-paths">
          ${La(`Repo path`,e.repoPath)}
          ${La(`MonoFS`,e.monofsPath)}
          ${La(`Payload`,e.payloadPath)}
          ${La(`Config`,e.configPath)}
        </div>
        ${e.repoPathHint?`<div class="assignment-meta">${i(e.repoPathHint)}</div>`:``}
        ${e.lastSyncError?`<div class="assignment-warning">${i(e.lastSyncError)}</div>`:``}
      </div>`}).join(``):`
    <div class="assignment-warning">
      No repository is assigned to this partition.
      <button class="btn-secondary" style="padding:2px 8px;font-size:10px;" data-assignment-action="connections">Bind repo</button>
    </div>`,m=l.length?l.map(e=>{let t=e.registered!==!1,n=Array.isArray(e.intents)?e.intents:[];return`
      <div class="assignment-target">
        <span class="status-dot status-dot-${t?`healthy`:`failing`}"></span>
        <div>
          <strong>${i(e.name??`pusher`)}</strong>
          <div class="meta">${e.account?`account ${i(e.account)} · `:``}${i(e.region??`region unknown`)}${t?``:` · not registered`}</div>
        </div>
        <span class="right">${n.map(e=>`<span class="assignment-chip">${i(e)}</span>`).join(``)}</span>
      </div>`}).join(``):`<div class="assignment-empty">No target pusher found for this partition.</div>`,h=u.length?`
    <div class="assignment-warnings">
      ${u.map(e=>`<div class="item">⚠ ${i(e)}</div>`).join(``)}
    </div>`:``;t.innerHTML=`
    <div class="assignment-block">
      <div class="assignment-block-head">
        <span class="assignment-title">Repository</span>
        <span class="right"><button class="btn-secondary" style="padding:2px 8px;font-size:10px;" data-assignment-action="connections">Manage in Connections</button></span>
      </div>
      ${p}
    </div>
    <div class="assignment-block">
      <div class="assignment-block-head">
        <span class="assignment-title">Targets</span>
        <span class="assignment-chip">${l.length} assigned</span>
      </div>
      ${m}
    </div>
    ${h}`}function La(e,t){let n=String(t??``).trim();return n?`<div class="assignment-path-row"><span class="label">${i(e)}</span><code>${i(n)}</code><button class="copy" data-copy="${a(n)}">copy</button></div>`:``}function Ra(e,t){return`
    <div class="stat-card rounded-lg border border-white/[0.09]">
      <div class="stat-label">${i(e)}</div>
      <div class="stat-value">${t}</div>
    </div>
  `}function za(e){for(let t of[`intentMapCard`,`serviceHealthCard`,`recentEventsCard`]){let n=document.getElementById(t);n&&(n.style.display=e?``:`none`)}}function Ba(){let e=document.getElementById(`attentionAssetsList`);if(!e)return;let t=(G.overview?.partitions??[]).filter(e=>{let t=String(e.health??``).toLowerCase();return t!==`healthy`&&t!==`observed`}).slice(0,12);if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Nothing needs attention right now.`;return}e.className=`attention-asset-list`,e.innerHTML=t.map(e=>`
    <article class="attention-asset-card attention-asset-card-${a(e.health??`attention`)}">
      <div class="attention-asset-card-header">
        <div>
          <h3>${i(e.name)}</h3>
          <div class="muted">${i(e.displayStatus??o(e.health??`attention`))} · ${e.intentCount??0} intents · ${e.assetCount??0} assets</div>
        </div>
        <button class="btn-secondary" data-global-partition="${a(e.name)}">Open →</button>
      </div>
      ${e.summary?`<p class="muted mt-1">${i(e.summary)}</p>`:``}
    </article>
  `).join(``),e.querySelectorAll(`[data-global-partition]`).forEach(e=>{e.addEventListener(`click`,()=>ba(e.dataset.globalPartition).catch($))})}function Va(){let e=document.getElementById(`attentionAssetsList`);if(!e)return;let t=Ha(),n=Ua();if(!t.length&&!n.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No assets need attention right now.`;return}e.className=`attention-asset-list`,e.innerHTML=t.map(({intent:e,asset:t})=>{let n=Wa(t);return`
    <article class="attention-asset-card attention-asset-card-${a(t.health)}">
      <div class="attention-asset-card-header">
        <div>
          <h3>${i(t.name)}</h3>
          <div class="muted">${i(e.name)} · ${i(as(t.type))}</div>
        </div>
        ${X(t.health,t.displayStatus,`${e.name} / ${t.name}`,t.summary,`asset:${G.selectedPartition}:${e.name}:${t.name}`)}
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
      ${n.map(({intent:e,asset:t})=>{let n=Wa(t);return`
        <div class="progressing-asset-item">
          <div>
            <div>${i(t.name)}</div>
            <div class="muted">${i(e.name)} · ${i(as(t.type))}${n?` · ${i(n)}`:``}</div>
          </div>
          ${X(t.health,t.displayStatus,`${e.name} / ${t.name}`,t.summary,`asset:${G.selectedPartition}:${e.name}:${t.name}`)}
        </div>
      `}).join(``)}
    </div>
  `:``)}function Ha(){return(G.detail?.intents??[]).flatMap(e=>(e.assets??[]).map(t=>({intent:e,asset:t}))).filter(({asset:e})=>e?.health===`failing`||e?.health===`attention`||e?.health===`drifted`||e?.health===`drifted-locked`).sort((e,t)=>{let n=Ga(e.asset.health)-Ga(t.asset.health);return n===0?e.intent.name===t.intent.name?e.asset.name.localeCompare(t.asset.name):e.intent.name.localeCompare(t.intent.name):n})}function Ua(){return(G.detail?.intents??[]).flatMap(e=>(e.assets??[]).map(t=>({intent:e,asset:t}))).filter(({asset:e})=>e?.health===`pending`).sort((e,t)=>e.intent.name===t.intent.name?e.asset.name.localeCompare(t.asset.name):e.intent.name.localeCompare(t.intent.name))}function Wa(e){let t=String(e?.summary??``).trim(),n=String(e?.observedHealth?.summary??``).trim(),r=String(e?.status??``);return(r===`Drifted`||r===`DriftedLocked`)&&n?t.includes(n)?t:t?`${t}: ${n}`:n:t}function Ga(e){return e===`failing`?0:e===`drifted-locked`?1:e===`drifted`?2:e===`attention`?3:4}function Ka(){let e=document.getElementById(`intentCards`);if(!e)return;let t=G.detail?.intents??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No intents defined for this partition yet.`;return}e.className=`intent-stack`,e.innerHTML=t.map(e=>{let t=ls(e.assets??[]),n=G.activityDrawer.intentName===e.name,r=us(e.assets??[]).map(t=>`
      <section class="intent-asset-group">
        <div class="intent-asset-group-title">
          <span class="intent-lane-group-dot" style="background:${cs(t.category)}"></span>
          <span>${i(t.category)} · ${t.assets.length}</span>
        </div>
        <div class="asset-grid">
          ${t.assets.map(t=>{let n=Wa(t),r=qa(e.name,t.name),o=G.expandedAssetKey===r,s=is(t.type),c=`asset-detail-${Ja(r)}`,l=[...t.quickFacts??[]].sort((e,t)=>e.label===`Release`?-1:+(t.label===`Release`)).map(e=>`<span class="fact${Yo(e.label)}" title="${a(Xo(e.label))}">${i(e.label)}: ${i(e.value)}</span>`).join(``),u=o?ns(t,{limit:2**53-1,truncateAt:160}):``,d=o?rs(t.outputs??{},[],{limit:2**53-1,truncateAt:160}):``,f=o?(t.references??[]).map(e=>`<span class="fact">${i(e)}</span>`).join(``):``,p=o&&(t.dependsOn??[]).length?(t.dependsOn??[]).map(e=>`<span class="fact">${i(e)}</span>`).join(``):``;return`
              <article
                class="asset-chip asset-chip-${a(t.health??`neutral`)}${o?` asset-chip-expanded`:``}"
                data-asset-toggle="${a(r)}"
                data-asset-card="${a(Ja(r))}"
                role="button"
                tabindex="0"
                aria-expanded="${o?`true`:`false`}"
                aria-controls="${a(c)}"
              >
                <div class="asset-chip-top">
                  <div>
                    <div class="asset-chip-title">${i(t.name)}</div>
                    <div class="asset-chip-type-row">
                      <span class="asset-chip-type">${i(as(t.type))}</span>
                      <span class="asset-chip-category">${i(s)}</span>
                      ${t.originLabel?`<span class="asset-origin asset-origin-${a(t.origin??`native`)}" title="${a(t.template?`${t.originLabel} · ${t.template}`:t.originLabel)}">${i(t.originLabel)}</span>`:``}
                    </div>
                  </div>
                  ${X(t.health,t.displayStatus,`${e.name} / ${t.name}`,t.summary,`asset:${G.selectedPartition}:${e.name}:${t.name}`)}
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
              ${e.originLabel?`<span class="pill origin-pill origin-${a(e.origin??`native`)}" title="${a(e.originDetail??``)}">${i(e.originLabel)}</span>`:``}
              ${X(e.health,e.displayStatus,`${e.name} intent`,e.summary,`intent:${G.selectedPartition}:${e.name}`)}
              ${(()=>{let t=Mo(e);return X(t.status,t.label,`${e.name} snapshot freshness`,t.detail,`intent-freshness:${G.selectedPartition}:${e.name}`)})()}
              ${e.lastDeployment?.createdAt?(()=>{let t=new Date(e.lastDeployment.createdAt).getTime(),n=Number.isFinite(t)?Date.now()-t:null,r=n!==null&&n>=0?`Applied ${Z(n)}`:`Applied ${c(e.lastDeployment.createdAt)}`;return`<span class="pill" title="${a(`Last deployment: ${c(e.lastDeployment.createdAt)}`)}">${i(r)}</span>`})():``}
              <span class="pill">${i(e.targetSummary??`Unassigned`)}</span>
              ${(e.joined??[]).map(e=>`<span class="pill">joins ${i(e)}</span>`).join(``)}
              ${t.map(e=>`<span class="pill">${i(`${e.category} ${e.count}`)}</span>`).join(``)}
              ${e.locked?`<span class="pill">locked</span>`:``}
              <button class="activity-btn ${n?`active`:``}" type="button" data-activity-intent="${a(e.name)}">&#9685;</button>
            </div>
          </div>
        </div>
        ${n?Qa():``}
        ${r}
      </article>
    `}).join(``),e.querySelectorAll(`[data-activity-intent]`).forEach(e=>{e.addEventListener(`click`,()=>Za(e.dataset.activityIntent).catch($))}),e.querySelectorAll(`[data-asset-toggle]`).forEach(e=>{e.addEventListener(`click`,()=>Ya(e.dataset.assetToggle??``)),e.addEventListener(`keydown`,t=>{t.key!==`Enter`&&t.key!==` `||(t.preventDefault(),Ya(e.dataset.assetToggle??``))})})}function qa(e,t){return`${e}::${t}`}function Ja(e){return e.replace(/[^a-zA-Z0-9_-]+/g,`-`)}function Ya(e){e&&(G.expandedAssetKey=G.expandedAssetKey===e?``:e,Ka())}function Xa(e){if(!e)return;G.expandedAssetKey=e,Ka();let t=Ja(e);requestAnimationFrame(()=>{document.querySelector(`[data-asset-card="${t}"]`)?.scrollIntoView({behavior:`smooth`,block:`center`,inline:`nearest`})})}async function Za(e){if(G.activityDrawer.intentName===e){G.activityDrawer={intentName:``,data:null,loading:!1,error:``},Ka();return}G.activityDrawer={intentName:e,data:null,loading:!0,error:``},Ka();try{let n=G.selectedPartition;G.activityDrawer={intentName:e,data:await t(`/api/partitions/${encodeURIComponent(n)}/intents/${encodeURIComponent(e)}/activity`),loading:!1,error:``}}catch(t){G.activityDrawer={intentName:e,data:null,loading:!1,error:t.message??`Failed to load activity`}}Ka()}function Qa(){let{data:e,loading:t,error:n}=G.activityDrawer;if(t)return`<div class="activity-drawer"><div class="activity-loading">Loading activity…</div></div>`;if(n)return`<div class="activity-drawer"><div class="activity-error">${i(n)}</div></div>`;if(!e)return`<div class="activity-drawer"><div class="activity-loading">No activity data.</div></div>`;let r=e.timestamps??{},o=[{label:`Queued`,value:r.lastQueuedAt},{label:`Check`,value:r.lastCheckAt},{label:`Diff`,value:r.lastDiffAt},{label:`Apply`,value:r.lastApplyAt}].filter(e=>e.value&&e.value!==`0001-01-01T00:00:00Z`),s=e.logs??[],l=e.drift,u=r.lastQueuedAt?Date.now()-new Date(r.lastQueuedAt).getTime():null,d=u!==null&&u>0?Z(u):null,f=``;return e.taskTimedOut?f=`
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
  `}function $a(){let e=document.getElementById(`serviceHealthCards`);if(!e)return;let t=G.detail?.health?.services??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No service-like assets to score yet.`;return}let n=t.filter(e=>e.status===`healthy`).length,r=t.filter(e=>e.status===`attention`).length,o=t.filter(e=>e.status===`failing`).length,s=t.filter(e=>e.taskActive).length,c=t.filter(e=>e.taskTimedOut).length;e.className=`service-stack`,e.innerHTML=`
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
          <div class="muted">${i(e.intent)} · ${i(as(e.type))}</div>
        </div>
        ${X(e.status,e.displayStatus,`${e.intent} / ${e.asset}`,e.summary,`service:${G.selectedPartition}:${e.intent}:${e.asset}`)}
      </div>
      <p class="service-card-note">${i(eo(e))}</p>
      <div class="service-health-meta">
        ${to(e)}
      </div>
      <div class="service-card-actions">
        <button class="btn-secondary service-card-action" type="button" data-service-focus="${a(qa(e.intent,e.asset))}">Open details</button>
      </div>
    </article>
  `).join(``)}
  `,e.querySelectorAll(`[data-service-focus]`).forEach(e=>{e.addEventListener(`click`,()=>Xa(e.dataset.serviceFocus??``))})}function eo(e){if(e.taskTimedOut)return`Last reconcile task timed out. Open details in the intent map for config and outputs.`;if(e.taskActive)return`Reconcile is currently running for this service.`;switch(e.status){case`healthy`:return`Operational summary only. Configuration and ports stay in the intent map.`;case`pending`:return`Waiting for the first successful reconcile.`;case`attention`:return String(e.summary??`Needs attention.`);case`failing`:return String(e.summary??`Service is failing.`);default:return String(e.summary??`Service status unavailable.`)}}function to(e){let t=[];e.taskActive&&t.push(`reconcile running`),e.taskTimedOut&&t.push(`last task timed out`);let n=c(e.lastUpdatedAt);return n!==`—`&&t.push(`updated ${n}`),t.map(e=>`<span class="service-health-meta-item">${i(e)}</span>`).join(``)}function no(){let e=document.getElementById(`recentEventsList`);if(!e)return;let t=G.detail?.recentEvents??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Recent event history loads only in the History tab.`;return}e.className=`timeline-stack`,e.innerHTML=Co(t).map(wo).join(``)}function q(){let e=document.getElementById(`deploymentTimeline`),t=document.getElementById(`eventTimeline`);if(!e||!t)return;if(!G.selectedPartition){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a partition to inspect deployment history.`,t.className=`empty-state text-sm text-[#566778]`,t.textContent=`Select a partition to inspect event history.`;return}if(G.historyLoading){e.className=`loading-state text-sm text-[#566778]`,e.textContent=`Loading deployment history…`,t.className=`loading-state text-sm text-[#566778]`,t.textContent=`Loading event history…`;return}if(G.historyError){e.className=`empty-state text-sm text-[#566778]`,e.textContent=G.historyError,t.className=`empty-state text-sm text-[#566778]`,t.textContent=G.historyError;return}let n=G.history;if(!n){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Open the History tab to load deployment history.`,t.className=`empty-state text-sm text-[#566778]`,t.textContent=`Open the History tab to load event history.`;return}let r=(document.getElementById(`historyFilter`)?.value??``).trim().toLowerCase(),i=(n.deployments??[]).filter(e=>r?`${e.intent} ${e.deploymentRevision} ${(e.assets??[]).map(e=>`${e.asset} ${e.version??``}`).join(` `)}`.toLowerCase().includes(r):!0),a=(n.events??[]).filter(e=>r?`${e.intent??``} ${e.title??``} ${e.message??``}`.toLowerCase().includes(r):!0);e.className=i.length?`timeline-stack`:`empty-state text-sm text-[#566778]`,e.innerHTML=i.length?i.map(To).join(``):`No deployment entries match the current filter.`;let o=document.getElementById(`historyGroupToggle`),s=!o||o.checked?Co(a):a;t.className=s.length?`timeline-stack`:`empty-state text-sm text-[#566778]`,t.innerHTML=s.length?s.map(wo).join(``):`No events match the current filter.`}function ro(e){let t=e.map(e=>new Date(e.createdAt).getTime()).filter(e=>Number.isFinite(e)),n=e.map(e=>ko(e.appliedAt)).filter(e=>e!==null),r=t.length?Math.min(...t):Date.now(),i=Math.max(...t,...n,Date.now()),a=i+Math.max(6e4,Math.floor((i-r||6e4)*.08));return{minTime:r,paddedMaxTime:a,span:Math.max(1,a-r)}}function io(e,t,n){return Math.min(100,Math.max(0,(e-t)/n*100))}function J(){let e=document.getElementById(`rolloutsTimeline`);if(!e)return;if(!G.selectedPartition){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a partition to inspect rollout history.`;return}if(G.rolloutsLoading){e.className=`loading-state text-sm text-[#566778]`,e.textContent=`Loading rollouts…`;return}if(G.rolloutsError){e.className=`empty-state text-sm text-[#566778]`,e.textContent=G.rolloutsError;return}let t=G.rollouts?.rollouts??[];if(!t.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`No archived rollouts were found for this partition yet.`;return}let{minTime:n,paddedMaxTime:r,span:a}=ro(t),o=new Map;for(let e of t){let t=String(e.intent??``).trim()||`(unknown intent)`,n=o.get(t);n?n.push(e):o.set(t,[e])}let s=Array.from(o.entries()).map(([e,t])=>({intent:e,items:t.slice().sort((e,t)=>new Date(e.createdAt).getTime()-new Date(t.createdAt).getTime())})).sort((e,t)=>{let n=new Date(e.items[e.items.length-1]?.createdAt??0).getTime();return new Date(t.items[t.items.length-1]?.createdAt??0).getTime()-n}),c=io(Date.now(),n,a),l=co(n,r,6),u=t.slice().sort((e,t)=>new Date(t.createdAt).getTime()-new Date(e.createdAt).getTime());e.className=`rollout-view`,e.innerHTML=`
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
        ${s.map(e=>oo(e,n,a)).join(``)}
      </div>
    </div>

    <!-- Event stream -->
    <div class="rollout-event-stream">
      <div class="rollout-event-stream-header">
        <span class="rollout-event-stream-title">Event stream</span>
        <span class="rollout-event-stream-count">${u.length} event${u.length===1?``:`s`}</span>
      </div>
      <div class="rollout-event-list">
        ${u.map(e=>ao(e)).join(``)}
      </div>
    </div>
  `,e.querySelectorAll(`[data-rollout-toggle]`).forEach(e=>{e.addEventListener(`click`,t=>{t.preventDefault();let n=e.dataset.rolloutToggle??``;n&&yo(n)})})}function ao(e){let t=go(e),n=bo(e),r=!!G.expandedRolloutKeys[n],s=(e.deploymentRevision??``).slice(0,8),l=e.assets??[],u=t.statusText,d=t.pointClass;e.newIntent?(u=`New intent`,d=`point-new`):e.rollback?(u=`Rollback`,d=`point-rollback`):e.selfHealing?(u=`Self-heal`,d=`point-heal`):e.current?e.healthStatus===`failing`?(u=`Error`,d=`point-failing`):e.healthStatus===`attention`?(u=`Degraded`,d=`point-degraded`):(u=`Deployed`,d=`point-current`):u=`Deployed`;let f=l.filter(e=>e.change&&e.change!==`unchanged`),p=f.length?f.slice(0,3).map(e=>i(e.name)).join(`, `)+(f.length>3?` +${f.length-3}`:``):l.length?`${l.length} asset${l.length===1?``:`s`}`:``;return`
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
            <span class="rollout-event-time" title="${a(c(e.createdAt))}">${i(fo(new Date(e.createdAt).getTime()))}</span>
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
              ${X(So(e.change),o(e.change||`updated`),void 0,void 0,void 0,vo(e.change))}
            </div>
          `).join(``)}
        </div>
      `:``}
    </div>
  `}function oo(e,t,n){let r=Date.now(),o=e.items.find(e=>e.current)??e.items[e.items.length-1],s=o?go(o):null,l=[];for(let i=0;i<e.items.length;i++){let a=e.items[i],o=i===e.items.length-1,s=new Date(a.createdAt).getTime(),c=Ao(a.appliedAt),u=o?Math.max(s,r):new Date(e.items[i+1].createdAt).getTime();if(o&&c!==null&&c>s&&c<u){let e=io(s,t,n),r=Math.max(e+.5,io(c,t,n)),i=Math.max(.5,r-e);l.push(`<div class="rollout-lane-segment seg-deploying" style="left:${e.toFixed(3)}%;width:${i.toFixed(3)}%" title="Deploying: ${fo(s)}"></div>`);let o=Math.max(r+.5,io(u,t,n)),d=Math.max(.5,o-r),f=go(a).segmentClass;l.push(`<div class="rollout-lane-segment ${f}" style="left:${r.toFixed(3)}%;width:${d.toFixed(3)}%"></div>`)}else{let r=io(s,t,n),c=Math.max(r+1,io(u,t,n)),d=Math.max(1,c-r),f=go(o?a:e.items[i+1]);l.push(`<div class="rollout-lane-segment ${f.segmentClass}" style="left:${r.toFixed(3)}%;width:${d.toFixed(3)}%"></div>`)}}let u=e.items.map((r,o)=>{let s=bo(r),l=!!G.expandedRolloutKeys[s],u=new Date(r.createdAt).getTime(),d=Math.max(.5,Math.min(99.5,io(u,t,n))),f=go(r),p=(r.deploymentRevision??``).slice(0,7);return`
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
    `}),d=ho(po(e.items)),f=s?`<span class="rollout-lane-status-chip chip-${s.pointClass.replace(`point-`,``)}" title="${a(s.statusText)}">${i(s.statusText)}</span>`:``;return`
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
        ${e.items.filter(e=>G.expandedRolloutKeys[bo(e)]).map(e=>so(e)).map(e=>`<div class="rollout-lane-card-wrap">${e}</div>`).join(``)}
      </div>
    </div>
  `}function so(e){let t=go(e),n=e.assets??[],r=n.length,s=bo(e),l=!!G.expandedRolloutKeys[s],u=e.healthText||t.statusText,d=e.healthSummary||``,f=X(t.statusKey,t.statusText,u,d,void 0,_o(t.statusText)),p=(e.deploymentRevision??``).slice(0,8),m=e.deploymentRevision??``;return`
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
          <span title="${a(c(e.createdAt))}">${i(fo(new Date(e.createdAt).getTime()))}</span>
          ${e.target?`<span>${i(e.target)}</span>`:``}
          ${e.summary?`<span class="rollout-card-summary-text">${i(e.summary)}</span>`:``}
        </div>

        ${xo(e)}
      </div>

      ${l&&r?`
        <div class="rollout-card-assets">
          ${n.map(t=>`
            <div class="rollout-card-asset">
              <div class="rollout-card-asset-name">${i(t.name)}</div>
              <div class="rollout-card-asset-meta">
                <span>${i(t.type||`Asset`)}</span>
                <span class="rollout-card-asset-version">${i(t.version||e.deploymentRevision)}</span>
                ${X(So(t.change),o(t.change||`updated`),void 0,void 0,void 0,vo(t.change))}
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
  `}function co(e,t,n){if(n<=1||t<=e)return[{left:0,label:lo(e)}];let r=[],i=t-e;for(let t=0;t<n;t++){let a=t/(n-1),o=e+i*a;r.push({left:Number((a*100).toFixed(3)),label:lo(o)})}return r}function lo(e){let t=new Date(e);return Number.isNaN(t.getTime())?`—`:`${t.toLocaleDateString(void 0,{month:`short`,day:`numeric`})}, ${t.toLocaleTimeString(void 0,{hour:`2-digit`,minute:`2-digit`,hour12:!1})}`}function uo(e){let t=new Date(e);return Number.isNaN(t.getTime())?`—`:t.toLocaleTimeString(void 0,{hour:`2-digit`,minute:`2-digit`,hour12:!1})}function fo(e){let t=new Date(e);return Number.isNaN(t.getTime())?`—`:t.toLocaleDateString(void 0,{month:`short`,day:`numeric`})+` `+t.toLocaleTimeString(void 0,{hour:`2-digit`,minute:`2-digit`,hour12:!1})}function po(e){let t=[],n=Date.now(),r=36e5,i=e.filter(e=>!!e.rollback);for(let a=0;a<e.length;a++){let o=e[a],s=e[a+1],c=Ao(o.createdAt);if(c===null)continue;let l=Ao(o.appliedAt),u=s?Ao(s.createdAt):null,d=s!=null&&!!s.rollback&&s.rollbackTo===o.deploymentRevision;if(o.rollback){let e=l||Math.min(c+6e4,u||c+6e4);e>c&&t.push({type:`recovering`,label:`Recover`,startMs:c,endMs:e,cssClass:`phase-recovering`,rollout:o});let i=u||(o.current?n:c+6e4),a=o.current?i:Math.min(i,e+r);if(a>e){let n=`healthy`,r=`phase-healthy`;o.healthStatus===`failing`?(n=`failing`,r=`phase-failing`):o.healthStatus===`attention`&&(n=`degraded`,r=`phase-degraded`);let i=o.current&&!s&&a-e>300*1e3;t.push({type:n,label:o.healthStatus===`failing`?`Failed`:o.healthStatus===`attention`?`Degraded`:`Healthy`,startMs:e,endMs:a,cssClass:i?r+` phase-compressed`:r,rollout:o})}continue}if(d){let e=u||c+3e4;t.push({type:`failing`,label:`Failed`,startMs:c,endMs:e,cssClass:`phase-failing`,rollout:o});continue}let f=l||Math.min(c+3e4,u||c+3e4),p=i.find(e=>{let t=Ao(e.createdAt);return t!==null&&t>c&&f!==null&&t<f});if(p&&l!==null){let e=Ao(p.createdAt),n=2e3;e>c+2e3&&t.push({type:`deploying`,label:`Deploy`,startMs:c,endMs:e,cssClass:`phase-deploying`,rollout:o}),t.push({type:`failing`,label:`Failed`,startMs:e,endMs:e+n,cssClass:`phase-failing`,rollout:o}),t.push({type:`recovering`,label:`Recover`,startMs:e+n,endMs:l,cssClass:`phase-recovering`,rollout:o})}else f>c&&t.push({type:`deploying`,label:`Deploy`,startMs:c,endMs:f,cssClass:`phase-deploying`,rollout:o});let m=u||(o.current?n:c+6e4),h=o.current?m:Math.min(m,f+r);if(h>f){let e=`healthy`,n=`phase-healthy`;o.healthStatus===`failing`?(e=`failing`,n=`phase-failing`):o.healthStatus===`attention`?(e=`degraded`,n=`phase-degraded`):o.selfHealing&&(e=`recovering`,n=`phase-recovering`);let r=o.current&&!s&&h-f>300*1e3;t.push({type:e,label:o.selfHealing?`Recover`:o.healthStatus===`failing`?`Failed`:o.healthStatus===`attention`?`Degraded`:`Healthy`,startMs:f,endMs:h,cssClass:r?n+` phase-compressed`:n,rollout:o})}}return t.sort((e,t)=>e.startMs-t.startMs),t}function mo(e){if(e<=0)return``;let t=Math.round(e/1e3);if(t<120)return t+`s`;if(t<3600)return Math.round(t/60)+`min`;if(t<86400){let e=Math.floor(t/3600),n=Math.round(t%3600/60);return n>0?e+`h `+n+`min`:e+`h`}let n=Math.floor(t/86400),r=Math.round(t%86400/3600);return n<30?r>0?n+`d `+r+`h`:n+`d`:Math.round(n/30)+`mo`}function ho(e){if(!e.length)return``;let t=e.map(e=>{let t=(e.endMs-e.startMs)/1e3;return e.type===`healthy`||e.type===`degraded`?Math.min(t,300):Math.max(t,20)}),n=t.reduce((e,t)=>e+t,0);return`
    <div class="rollout-phase-bar">
      ${e.map((r,o)=>{let s=n>0?Math.max(2,t[o]/n*100):100/e.length,c=mo(r.endMs-r.startMs);return c?`${r.label}${c}`:r.label,`<div class="rollout-phase ${r.cssClass}" style="flex:0 1 ${s}%" title="${a(r.label+`: `+uo(r.startMs)+` → `+uo(r.endMs)+(c?` (`+c+`)`:``))}"><span>${i(r.label)}</span>${c?`<span class="rollout-phase-duration">${c}</span>`:``}</div>`}).join(``)}
    </div>
  `}function go(e){return e.current?e.rollback?{statusText:`Current (rollback)`,statusKey:`attention`,cardClass:`card-current card-rollback`,segmentClass:`seg-rollback`,pointClass:`point-rollback`}:e.healthStatus===`failing`?{statusText:`Error`,statusKey:`failing`,cardClass:`card-current card-failing`,segmentClass:`seg-failing`,pointClass:`point-failing`}:e.healthStatus===`attention`?{statusText:`Degraded`,statusKey:`attention`,cardClass:`card-current card-degraded`,segmentClass:`seg-degraded`,pointClass:`point-degraded`}:e.healthStatus===`healthy`||e.healthStatus===`ready`?{statusText:`Current`,statusKey:`healthy`,cardClass:`card-current`,segmentClass:`seg-current`,pointClass:`point-current`}:e.newIntent?{statusText:`New intent`,statusKey:`pending`,cardClass:`card-current card-new`,segmentClass:`seg-new`,pointClass:`point-new`}:{statusText:`Current`,statusKey:`healthy`,cardClass:`card-current`,segmentClass:`seg-current`,pointClass:`point-current`}:e.newIntent?{statusText:`New intent`,statusKey:`pending`,cardClass:`card-new`,segmentClass:`seg-new`,pointClass:`point-new`}:e.rollback?{statusText:`Rollback`,statusKey:`attention`,cardClass:`card-rollback`,segmentClass:`seg-rollback`,pointClass:`point-rollback`}:e.selfHealing?{statusText:`Self-heal`,statusKey:`neutral`,cardClass:``,segmentClass:`seg-heal`,pointClass:`point-heal`}:{statusText:`Superseded`,statusKey:`neutral`,cardClass:``,segmentClass:`seg-old`,pointClass:`point-old`}}function _o(e){switch(e){case`Current`:return`This is the latest successful rollout revision for the intent.`;case`Current (rollback)`:return`The intent was rolled back to this revision after a failed deploy.`;case`Error`:return`The current rollout has failing assets (e.g. ImagePullBackOff, CrashLoopBackOff).`;case`Degraded`:return`The current rollout has assets that are not fully ready.`;case`New intent`:return`The intent appeared for the first time in this rollout.`;case`Self-heal`:return`Guardian reconciled drift and re-applied the desired state without a new intent change.`;case`Rollback`:return`Guardian reverted to a previously known-good deployment revision.`;case`Recover`:return`Guardian is re-deploying after a failed APPLY attempt (auto-heal rollout).`;default:return`An older rollout kept for history; it is no longer the active revision.`}}function vo(e){switch((e??``).toLowerCase()){case`added`:return`This asset was introduced in this rollout.`;case`removed`:return`This asset was removed in this rollout.`;case`refreshed`:return`Guardian re-applied this asset during self-heal to restore desired state.`;case`updated`:return`This asset definition or effective version changed in this rollout.`;default:return`This asset participated in this rollout.`}}function yo(e){e&&(G.expandedRolloutKeys={...G.expandedRolloutKeys,[e]:!G.expandedRolloutKeys[e]},J())}function bo(e){return`${e.intent??``}::${e.deploymentRevision??``}`}function xo(e){if(!e.rollback)return``;let t=[];e.rollbackTo&&t.push(`Rolled back to revision <code>${i(e.rollbackTo)}</code>`);let n=String(e.rollbackReason??``).trim();return n&&t.push(`Reason: ${i(n)}`),t.length?`<div class="rollout-tl-row"><div class="rollout-tl-rollback-detail">${t.join(` &middot; `)}</div></div>`:``}function So(e){switch((e??``).toLowerCase()){case`added`:return`pending`;case`removed`:return`attention`;default:return`healthy`}}function Co(e){let t=new Map;for(let n of e){let e=`${n.intent??``}::${n.title}`,r=t.get(e);!r||new Date(n.timestamp)>new Date(r.latest.timestamp)?t.set(e,{latest:n,count:(r?.count??0)+1}):r.count++}return Array.from(t.values()).sort((e,t)=>new Date(t.latest.timestamp).getTime()-new Date(e.latest.timestamp).getTime()).map(e=>({...e.latest,groupCount:e.count}))}function wo(e){let t=e.groupCount>1?`<span class="event-count-pill" title="${e.groupCount} occurrences">${e.groupCount}×</span>`:``,n=(e.title??``).toLowerCase().replace(/[^a-z0-9]/g,``),r=(e.message??``).toLowerCase().replace(/[^a-z0-9]/g,``),a=e.message&&r!==n;return`
    <article class="timeline-card">
      <div class="timeline-head">
        <div>
          <span class="event-type-eyebrow">Event type</span>
          <h3>${i(e.title??`Event`)} ${t}</h3>
          ${a?`<div class="muted">${i(e.message)}</div>`:``}
        </div>
        ${X(e.status,e.displayStatus,e.title??`Event`,e.message??``)}
      </div>
      <div class="timeline-meta">
        <span>${c(e.timestamp)}</span>
        ${e.intent?`<span>${i(e.intent)}</span>`:``}
        ${e.taskID?`<span>${i(e.taskID)}</span>`:``}
        ${e.deploymentRevision?`<span>${i(e.deploymentRevision)}</span>`:``}
      </div>
    </article>
  `}function To(e){return`
    <article class="timeline-card">
      <div class="timeline-head">
        <div>
          <h3>${i(e.intent)}</h3>
          <div class="muted">${i(e.deploymentRevision)}</div>
        </div>
        ${X(e.status,e.displayStatus)}
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
              ${X(e.status,e.displayStatus)}
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
  `}function Eo(){let e=document.getElementById(`topologyCanvas`);if(!e)return;let t=G.detail?.topology;g(document.getElementById(`topologyLegend`)),se({canvas:e,topology:t,zoom:G.topology.zoom,savedPositions:G.topology.nodePositions,selectedNodeId:G.topology.selectedNodeId,filters:{contains:document.getElementById(`showContainEdges`)?.checked??!0,join:document.getElementById(`showJoinEdges`)?.checked??!0,dependsOn:document.getElementById(`showAssetEdges`)?.checked??!0,outputRef:document.getElementById(`showOutputEdges`)?.checked??!0},onSelectNode:(e,t)=>{G.topology.selectedNodeId=e,G.topology.nodePositions=t,Do(),Eo()},onDragNode:(e,t)=>{G.topology.nodePositions=t}}),Do()}function Do(){let e=document.getElementById(`topologyDetails`);if(!e)return;let t=G.detail?.topology;if(!t?.nodes?.length){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a node to inspect its status, metadata, and linked details.`;return}let n=new Map(t.nodes.map(e=>[e.id,e])),r=n.get(G.topology.selectedNodeId);if(!r){e.className=`empty-state text-sm text-[#566778]`,e.textContent=`Select a node to inspect its status, metadata, and linked details.`;return}let a={contains:document.getElementById(`showContainEdges`)?.checked??!0,join:document.getElementById(`showJoinEdges`)?.checked??!0,dependsOn:document.getElementById(`showAssetEdges`)?.checked??!0,outputRef:document.getElementById(`showOutputEdges`)?.checked??!0},s=(t.edges??[]).filter(e=>a[e.kind]!==!1).filter(e=>e.from===r.id||e.to===r.id),c=r.kind===`asset`?Vo(r.intent,r.asset??r.label):null,l=r.kind===`intent`?Bo(r.intent??r.label):null,u=c?ns(c):``,d=rs(c?.outputs??l?.outputs??{},c?[]:l?.outputHints??[]),f=(c?.references??[]).map(e=>`<span class="fact">${i(e)}</span>`).join(``),p=s.map(e=>{let t=e.from===r.id?e.to:e.from,a=n.get(t);return a?`
      <div class="topology-detail-row">
        <span class="topology-detail-direction">${e.from===r.id?`out`:`in`}</span>
        <span class="topology-detail-name">${i(a.label)}</span>
        <span class="topology-detail-kind">${i(o(e.kind))}</span>
      </div>
    `:``}).join(``);e.className=`topology-detail-card`,e.innerHTML=`
    <div style="--node-accent:${Ho(r)}">
      <div class="topology-detail-header">
        <div class="topology-detail-icon">${i(Uo(r))}</div>
        <div>
          <h3>${i(r.label)}</h3>
          <p>${i(Wo(r))}</p>
        </div>
      </div>
      <div class="pill-row mb-2">
        ${X(r.health??r.status,r.displayStatus??o(r.kind),r.label,r.description,`topology:${G.selectedPartition}:${r.id}`)}
        <span class="pill">${i(o(r.kind))}</span>
        ${r.assetType?`<span class="pill">${i(as(r.assetType))}</span>`:``}
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
  `}var Oo={overviewPanel:{eyebrow:`Control center`,title:`Overview`},setPanel:{eyebrow:`Set`,title:`Set`},registryPanel:{eyebrow:`Control center`,title:`Connections`},unmanagedPanel:{eyebrow:`Control center`,title:`Adopt`},scanPanel:{eyebrow:`Control center`,title:`Adopt`},builderPanel:{eyebrow:`Visual editor`,title:`Build`},topologyPanel:{eyebrow:`Deployment graph`,title:`Topology`},rolloutsPanel:{eyebrow:`Release timeline`,title:`Rollouts`},historyPanel:{eyebrow:`Push timeline`,title:`History`}};function Y(){let e=G.activePanel,t=G.detail,r=Oo[e]??Oo.overviewPanel,o=e===`overviewPanel`,s=!!G.selectedPartition,c=o&&!s,l=(e,t)=>{let n=document.getElementById(e);n&&(n.style.display=t?``:`none`)},u=[`overviewPanel`,`topologyPanel`,`rolloutsPanel`,`historyPanel`].includes(e);l(`appGridSection`,c),l(`setCardsSection`,c),l(`summaryGrid`,!1),l(`selectedPartitionHero`,o&&s),l(`sidebarPartitionSection`,!0);let d=document.getElementById(`overviewHealthLine`);d&&(d.style.display=c?`flex`:`none`);let f=document.getElementById(`partitionTabs`);f&&(f.style.display=u&&s?`flex`:`none`),n(`pageEyebrow`,r.eyebrow),n(`pageTitle`,e===`setPanel`?Ti():e===`overviewPanel`&&s?G.selectedPartition:r.title);let p=`Monitor partitions, inspect topology, and review history.`,m=``;if(e===`overviewPanel`&&t){p=`${t.partition.manifest.spec?.deletionPolicy??`orphan`} policy · ${t.partition.manifest.spec?.reconciliation?.mode??`manual`} reconcile · ${t.intents.length} intents`;let e=No(t),n=X(e.status,e.label,`${t.partition.manifest.metadata.name} snapshot freshness`,e.detail,`partition-freshness:${t.partition.manifest.metadata.name}`);m=`${X(t.health?.status,t.health?.displayStatus??`Selected`,`${t.partition.manifest.metadata.name} health`,t.health?.summary,`partition-health:${t.partition.manifest.metadata.name}`)} ${n} ${t.originLabel?`<span class="pill origin-pill origin-${a(t.origin??`native`)}" title="${a(t.originDetail??``)}">${i(t.originLabel)}</span>`:``} <span class="pill">${t.topology?.nodes?.length??0} nodes</span>`}e===`builderPanel`&&(p=t?`Compose intents and assets for ${t.partition.manifest.metadata.name}.`:`Compose a new partition or edit an existing one.`,m=t?`<span class="pill">${i(t.partition.manifest.metadata.name)}</span><span class="pill">${t.intents?.length??0} intents</span>`:``),e===`registryPanel`&&(p=`Agents, source repositories, and partition bindings.`),e===`setPanel`&&(p=`Named group of partitions; membership is virtual.`,m=``),(e===`unmanagedPanel`||e===`scanPanel`)&&(p=`Bring existing AWS stacks under repository ownership.`),e===`topologyPanel`&&(p=t?`Topology for ${t.partition.manifest.metadata.name}.`:`Select a partition to inspect its graph.`,m=t?`<span class="pill">${i(t.partition.manifest.metadata.name)}</span><span class="pill">${t.topology?.nodes?.length??0} nodes</span>`:``),e===`rolloutsPanel`&&(p=t?`Review archived rollout changes for ${t.partition.manifest.metadata.name}.`:`Select a partition to inspect rollout history.`,m=t?`<span class="pill">${i(t.partition.manifest.metadata.name)}</span><span class="pill">${G.rollouts?.rollouts?.length??0} rollouts</span>`:``),e===`historyPanel`&&(p=t?`Review deployments and state changes for ${t.partition.manifest.metadata.name}.`:`Select a partition to inspect history.`,m=t?`<span class="pill">${i(t.partition.manifest.metadata.name)}</span><span class="pill">${G.history?.deployments?.length??0} deployments</span>`:``),n(`pageSubtitle`,p);let h=document.getElementById(`headerContextPills`);h&&(h.innerHTML=m,h.style.display=m.trim()?``:`none`);let ee=document.getElementById(`topnavPartition`),te=e===`setPanel`?Ti():u&&G.selectedPartition?G.selectedPartition:r.title||`Overview`;ee&&(ee.textContent=te)}function X(e,t,n,r,s,c){let l=String(e??`neutral`).toLowerCase(),u=t??o(l),d=Po(s,l,r),f=(l===`failing`||l===`attention`||l===`drifted`||l===`drifted-locked`)&&d.length>0,p=c?` title="${a(c)}"`:``;return f?`<button type="button" class="badge badge-${a(l)} badge-clickable" data-diagnostic-title="${a((n??u).trim())}" data-diagnostic-detail="${a(d)}" aria-label="Show diagnostic details for ${a(u)}"${p}>${i(u)}</button>`:`<span class="badge badge-${a(l)}"${p}>${i(u)}</span>`}function ko(e){if(!e)return null;let t=String(e);if(t===`0001-01-01T00:00:00Z`||t===``)return null;let n=Date.parse(t);return Number.isFinite(n)?n:null}function Ao(e){if(!e)return null;let t=Date.parse(String(e));return Number.isFinite(t)?t:null}function jo(e){let t=e?.state?.timestamps??{},n=[ko(t.lastCheckAt),ko(t.lastApplyAt),ko(t.lastDiffAt),ko(t.lastQueuedAt)].filter(e=>e!==null);return n.length===0?null:Math.max(...n)}function Z(e){let t=Math.max(0,Math.floor(e/1e3));if(t<60)return`${t}s ago`;let n=Math.floor(t/60);if(n<60)return`${n}m ago`;let r=Math.floor(n/60);return r<24?`${r}h ago`:`${Math.floor(r/24)}d ago`}function Mo(e){let t=jo(e);if(t===null)return{status:`neutral`,label:`No sample`,detail:`No reconciliation timestamps are available yet for this intent.`};let n=Date.now()-t,r=n>sa;return{status:r?`attention`:`healthy`,label:r?`Stale ${Z(n)}`:`Fresh ${Z(n)}`,detail:`Last sampled ${Z(n)} at ${c(new Date(t).toISOString())}.`}}function No(e){let t=(Array.isArray(e?.intents)?e.intents:[]).map(e=>jo(e)).filter(e=>e!==null);if(t.length===0)return{status:`neutral`,label:`No samples`,detail:`No intent reconciliation timestamps are available for this partition.`};let n=Math.max(...t),r=Date.now()-n,i=r>sa;return{status:i?`attention`:`healthy`,label:i?`Stale snapshot ${Z(r)}`:`Fresh snapshot ${Z(r)}`,detail:`Most recent intent sample is ${Z(r)} old (${c(new Date(n).toISOString())}).`}}function Po(e,t,n){let r=String(e??``).trim(),i=String(n??``).trim();return r?t===`failing`||t===`attention`||t===`drifted`||t===`drifted-locked`?i?(G.diagnosticDetails[r]=i,i):G.diagnosticDetails[r]??``:(delete G.diagnosticDetails[r],i):i}function Fo(){let e=document.getElementById(`diagnosticsModal`);if(e)return e;let t=document.createElement(`div`);return t.id=`diagnosticsModal`,t.className=`diagnostics-modal hidden`,t.innerHTML=`
    <div class="diagnostics-modal-card" role="dialog" aria-modal="true" aria-labelledby="diagnosticsModalTitle">
      <div class="diagnostics-modal-header">
        <h3 id="diagnosticsModalTitle">Status details</h3>
        <button type="button" class="diagnostics-close" data-diagnostics-close="true" aria-label="Close diagnostics">×</button>
      </div>
      <pre id="diagnosticsModalBody" class="diagnostics-modal-body"></pre>
    </div>
  `,t.addEventListener(`click`,e=>{let n=e.target;(n===t||n.closest(`[data-diagnostics-close='true']`))&&Lo()}),document.body.appendChild(t),t}function Io(e,t){let n=Fo(),r=n.querySelector(`#diagnosticsModalTitle`),i=n.querySelector(`#diagnosticsModalBody`);r&&(r.textContent=e.trim()||`Status details`),i&&(i.textContent=t.trim()),n.classList.remove(`hidden`),document.body.classList.add(`diagnostics-open`)}function Lo(){let e=document.getElementById(`diagnosticsModal`);e&&(e.classList.add(`hidden`),document.body.classList.remove(`diagnostics-open`))}function Ro(e){let t=e.partition?.manifest?.spec?.reconciliation?.mode??`manual`,n=e.partition?.manifest?.spec?.labels?.managedBy??``,r=(e.intents??[]).some(e=>e.targetSummary&&e.targetSummary!==`Unassigned`),i=e.health?.pending??0,a=[];return a.push(`${i} asset${i===1?` is`:`s are`} in <strong>Planned</strong> state — no reconcile has run yet.`),n===`external-compose`&&a.push(`Resources in this partition are managed externally by Docker Compose.`),t===`manual`?r?a.push(`Click <strong>Reconcile now</strong> in the sidebar to run the first reconcile and deploy assets.`):a.push(`No pusher is assigned. Assets will stay in Planned state until a pusher is configured.`):a.push(`The reconciler will process these assets automatically in the next cycle.`),`<div class="info-callout mt-2"><span class="info-callout-icon">?</span><div><strong>Why is this partition Progressing?</strong><p>${a.join(` `)}</p></div></div>`}function zo(e){let t=[`component`,`role`,`stack`,`topology`],n=[];return t.forEach(t=>{e[t]&&n.push(e[t])}),[...new Set(n)]}function Bo(e){return(G.detail?.intents??[]).find(t=>t.name===e)??null}function Vo(e,t){return Bo(e)?.assets?.find(e=>e.name===t)??null}var Q={partition:`#F0E442`,intent:`#CC79A7`,runtime:`#0072B2`,config:`#009E73`,storage:`#56B4E9`,traffic:`#D55E00`,muted:`#8B949E`};function Ho(e){return e.kind===`partition`?Q.partition:e.kind===`intent`?Q.intent:ss(e.assetType??e.kind)}function Uo(e){return e.kind===`partition`?`◫`:e.kind===`intent`?`⊞`:os(e.assetType??e.kind)}function Wo(e){return e.kind===`partition`?`${e.meta?.reconciliation??`manual`} reconcile · ${e.meta?.deletionPolicy??`orphan`} delete`:e.kind===`intent`?e.meta?.target??e.displayStatus??`Intent`:`${as(e.assetType??e.kind)} · ${e.displayStatus??`Asset`}`}var Go=[`Compute`,`Network`,`Config`,`Storage`,`Operations`],Ko={Compute:Q.runtime,Volume:Q.storage,Config:Q.config,ObjectStore:Q.storage,Database:Q.traffic,SQLDatabase:Q.traffic,LoadBalancer:Q.traffic,Observability:Q.config},qo={Image:`Container image reference (registry/name:tag@digest)`,Scale:`Desired replica count`,Env:`Environment variables injected at runtime`,Config:`ConfigMap or file mounts`,Storage:`Persistent volume mounts`,Ports:`Exposed service ports`,Port:`Service listener port`,Health:`Health check probe is configured`,CPU:`CPU resource limit/request`,Memory:`Memory resource limit/request`,Engine:`Storage engine or database type`,Version:`Engine version`,Database:`Database name`,Mode:`Deployment or storage mode`,Endpoint:`Connection endpoint address`,Size:`Volume storage capacity`,Access:`Volume access mode (e.g. ReadWriteOnce)`,Format:`Config file format (yaml / json / env)`,Files:`Config file definitions`,Inline:`Config data is defined inline in the manifest`,Targets:`Number of load balancer backend targets`,Listeners:`Number of load balancer listeners`,Buckets:`Object storage bucket names`,Provider:`Observability provider`,Receivers:`Telemetry input protocols`,Exporters:`Telemetry export destinations`,Outputs:`Output keys exposed to dependent intents`},Jo={Scale:`fact-scale`,Ports:`fact-port`,Port:`fact-port`,CPU:`fact-resource`,Memory:`fact-resource`,Env:`fact-env`,Storage:`fact-storage`,Size:`fact-storage`,Engine:`fact-engine`,Version:`fact-engine`,Outputs:`fact-outputs`};function Yo(e){return e===`Release`?`fact-release`:Jo[e]?` ${Jo[e]}`:``}function Xo(e){return qo[e]??e}function Zo(e){return(G.catalog?.assetTypes??[]).find(t=>t.type===e)??null}function Qo(e){return e.replace(/\[\d+\]/g,`[]`)}function $o(e,t){let n=Qo(t),r=n.replace(/\[\]/g,``).split(`.`)[0];return(e??[]).find(e=>e.path===n||e.path===r)??null}function es(e,t){let n=Zo(e?.type??``);return $o(e?.hints,t)??$o(n?.hints,t)??$o(n?.fields,t)??null}function ts(e,t=``){if(e==null||e===``)return[];if(Array.isArray(e))return e.length?e.flatMap((e,n)=>ts(e,`${t}[${n}]`)):t?[{path:t,value:`[]`}]:[];if(typeof e==`object`){let n=Object.entries(e);return n.length?n.flatMap(([e,n])=>ts(n,t?`${t}.${e}`:e)):t?[{path:t,value:`{}`}]:[]}return t?[{path:t,value:String(e)}]:[]}function ns(e,t={}){let n=t.limit??16,r=t.truncateAt??48,o=Zo(e?.type??``),c=new Map;(o?.fields??[]).forEach((e,t)=>{c.set(e.path,t)});let l=ts(e?.properties??{}).sort((e,t)=>{let n=Qo(e.path),r=Qo(t.path),i=n.replace(/\[\]/g,``).split(`.`)[0],a=r.replace(/\[\]/g,``).split(`.`)[0],o=c.get(n)??c.get(i)??2**53-1,s=c.get(r)??c.get(a)??2**53-1;return o===s?e.path.localeCompare(t.path):o-s});if(!l.length)return``;let u=l.slice(0,n),d=u.map(t=>{let n=es(e,t.path);return`<span class="fact" title="${a([t.path,n?.title,n?.description].filter(Boolean).join(` - `))}">${i(`${t.path}: ${s(t.value,r)}`)}</span>`}).join(``);if(l.length===u.length)return d;let f=l.length-u.length;return`${d}<span class="fact" title="${f} more properties available">+${f} more</span>`}function rs(e,t=[],n={}){let r=Object.entries(e??{}),o=n.limit??r.length,c=n.truncateAt??2**53-1,l=r.slice(0,o).map(([e,n])=>{let r=$o(t,`outputs.${e}`);return`<span class="fact" title="${a([`outputs.${e}`,r?.title,r?.description].filter(Boolean).join(` - `))}">${i(`${e}: ${s(String(n),c)}`)}</span>`}).join(``);if(r.length<=o)return l;let u=r.length-o;return`${l}<span class="fact" title="${u} more outputs available">+${u} more</span>`}function is(e){return Zo(e)?.category??`Other`}function as(e){return Zo(e)?.title??o(e)}function os(e){return Zo(e)?.icon??`⬡`}function ss(e){return Ko[e]??Q.muted}function cs(e){let t={Compute:Q.runtime,Network:Q.traffic,Config:Q.config,Storage:Q.storage,Operations:Q.config,Other:Q.muted};return t[e]??t.Other}function ls(e){let t=new Map;return e.forEach(e=>{let n=is(e.type);t.set(n,(t.get(n)??0)+1)}),[...t.keys()].sort((e,t)=>{let n=Go.indexOf(e),r=Go.indexOf(t);return n===-1&&r===-1?e.localeCompare(t):n===-1?1:r===-1?-1:n-r}).map(e=>({category:e,count:t.get(e)}))}function us(e){let t=new Map;return e.forEach(e=>{let n=is(e.type);t.has(n)||t.set(n,[]),t.get(n).push(e)}),[...t.keys()].sort((e,t)=>{let n=Go.indexOf(e),r=Go.indexOf(t);return n===-1&&r===-1?e.localeCompare(t):n===-1?1:r===-1?-1:n-r}).map(e=>({category:e,assets:t.get(e).sort((e,t)=>e.name.localeCompare(t.name))}))}function ds(){document.querySelectorAll(`[data-tab-target]`).forEach(e=>{e.addEventListener(`click`,()=>K(e.dataset.tabTarget,e.dataset.openScope))}),ve($),$e($),xt($),document.getElementById(`adoptScanButton`)?.addEventListener(`click`,()=>K(`scanPanel`)),wi({getOverview:()=>G.overview,getSelectedPartition:()=>G.selectedPartition,getSelectedSet:()=>G.selectedSet,onSelectPartition:e=>{G.selectedSet=``,K(`overviewPanel`),ba(e).catch($)},onOpenSet:e=>{G.selectedSet=e,e?K(`setPanel`):(G.selectedPartition=``,K(`overviewPanel`))},onOpenConnections:()=>K(`registryPanel`),onChanged:()=>Y(),onToast:d,onError:$}),document.querySelectorAll(`[data-adopt-tab]`).forEach(e=>{e.addEventListener(`click`,()=>K(e.dataset.adoptTab??`unmanagedPanel`))}),document.getElementById(`assignmentCard`)?.addEventListener(`click`,e=>{let t=e.target,n=t.closest(`[data-copy]`);if(n){let e=n.dataset.copy??``;navigator.clipboard?.writeText(e).then(()=>d(`Copied to clipboard`,`success`)).catch(()=>d(`Could not copy`,`error`));return}t.closest(`[data-assignment-action]`)?.dataset.assignmentAction===`connections`&&K(`registryPanel`)}),document.getElementById(`refreshButton`)?.addEventListener(`click`,()=>ya(!0).catch($)),document.getElementById(`reconcileButton`)?.addEventListener(`click`,ps),document.getElementById(`overviewCreatePartitionButton`)?.addEventListener(`click`,()=>{K(`builderPanel`,`new`)}),document.getElementById(`appGridSearch`)?.addEventListener(`input`,Pa),document.getElementById(`historyFilter`)?.addEventListener(`input`,q),document.getElementById(`historyGroupToggle`)?.addEventListener(`change`,q),document.getElementById(`historyApply`)?.addEventListener(`click`,()=>Da().catch($)),document.getElementById(`historyReset`)?.addEventListener(`click`,()=>Oa().catch($)),document.getElementById(`refreshRolloutsButton`)?.addEventListener(`click`,()=>{Aa(!0).catch($),_a()}),[`showContainEdges`,`showJoinEdges`,`showAssetEdges`,`showOutputEdges`].forEach(e=>{document.getElementById(e)?.addEventListener(`change`,Eo)}),document.getElementById(`topologyZoomOut`)?.addEventListener(`click`,()=>ms(-.1)),document.getElementById(`topologyZoomIn`)?.addEventListener(`click`,()=>ms(.1)),document.getElementById(`topologyResetLayout`)?.addEventListener(`click`,()=>{G.topology.nodePositions={},Eo()}),document.addEventListener(`click`,e=>{let t=e.target.closest(`[data-diagnostic-detail]`);t&&(e.preventDefault(),Io(t.dataset.diagnosticTitle??`Status details`,t.dataset.diagnosticDetail??`No diagnostic details were provided.`))}),document.addEventListener(`keydown`,e=>{e.key===`Escape`&&Lo()}),Fo(),fs()}function fs(){let e=document.getElementById(`refreshSlider`),t=document.getElementById(`refreshIntervalLabel`),n=document.getElementById(`refreshPopover`),r=document.getElementById(`syncIndicator`);if(!e||!n||!r)return;let i=Math.round(G.refreshIntervalMs/1e3);e.value=String(Math.min(ta/1e3,Math.max(ea/1e3,i))),t&&(t.textContent=`${e.value}s`);function a(){n.classList.contains(`hidden`)?n.classList.remove(`hidden`):n.classList.add(`hidden`)}r.addEventListener(`click`,e=>{e.stopPropagation(),a()}),e.addEventListener(`input`,()=>{let n=Number(e.value);t&&(t.textContent=`${n}s`)}),e.addEventListener(`change`,()=>{let t=Number(e.value);if(!(!Number.isFinite(t)||t<1)){G.refreshIntervalMs=t*1e3;try{localStorage.setItem($i,String(G.refreshIntervalMs))}catch{}pa()}}),document.addEventListener(`click`,e=>{if(n.classList.contains(`hidden`))return;let t=e.target;!n.contains(t)&&t!==r&&n.classList.add(`hidden`)})}async function ps(){let e=G.selectedPartition;if(!e){d(`Select a partition first.`,`error`);return}await t(`/api/partitions/${encodeURIComponent(e)}/reconcile`,{method:`POST`}),ga(),d(`Reconciliation requested.`,`success`),await ya(!1),await ba(e,!1)}function ms(e){let t=document.getElementById(`topologyCanvas`),n=gs(G.topology.zoom,.4,2.5),r=gs(Math.round((n+e)*100)/100,.4,2.5);if(n===r)return;let i=t?t.scrollLeft+t.clientWidth/2:0,a=t?t.scrollTop+t.clientHeight/2:0;if(G.topology.zoom=r,Eo(),t){let e=r/n;t.scrollLeft=Math.max(0,i*e-t.clientWidth/2),t.scrollTop=Math.max(0,a*e-t.clientHeight/2)}hs()}function hs(){let e=G.topology.zoom,t=document.getElementById(`topologyZoomOut`),n=document.getElementById(`topologyZoomIn`),r=document.getElementById(`topologyZoomValue`);r&&(r.textContent=`${Math.round(e*100)}%`),t&&(t.disabled=e<=.4),n&&(n.disabled=e>=2.5)}function gs(e,t,n){return Math.min(n,Math.max(t,e))}function $(e){d(e?.message??`Unexpected error`,`error`)}async function _s(){try{G.catalog=await t(`/api/catalog`)}catch{}}_s();