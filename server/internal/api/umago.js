(function(){var d=document,s=d.currentScript;if(!s)return;function attr(n,f){var v=s.getAttribute('data-'+n);return v==null?f:v}function opt(n){return s.hasAttribute('data-'+n)}var i=attr('site',null);if(!i||navigator.webdriver)return;
if(opt('do-not-track')&&(navigator.doNotTrack==='1'||navigator.doNotTrack==='yes'||window.doNotTrack==='1'))return;
var dm=attr('domains',null);if(dm){var ok=false,h=location.hostname.toLowerCase(),ds=dm.split(',');for(var k=0;k<ds.length;k++){var x=ds[k].trim().toLowerCase();if(x&&(h===x||h.slice(-x.length-1)==='.'+x)){ok=true;break}}if(!ok)return}
var e=(attr('host-url',null)||s.src.split(/[?#]/)[0].replace(/\/[^\/]*$/,''))+'/api/v1/collect',l=null,id=null,exS=opt('exclude-search'),exH=opt('exclude-hash'),tag=attr('tag',null);
function url(){var u=location.href;try{var p=new URL(u);if(exS)p.search='';if(exH)p.hash='';u=p.toString()}catch(_){}return u}
function hook(p){var f=attr('before-send',null);if(!f)return p;try{var fn=window[f];if(typeof fn==='function'){var r=fn(p);return r==null?null:r}}catch(_){}return p}
function post(b){b=hook(b);if(!b)return;var j=JSON.stringify(b);if(navigator.sendBeacon&&navigator.sendBeacon(e,j))return;fetch(e,{method:'POST',body:j,keepalive:true,headers:{'Content-Type':'text/plain'}}).catch(function(){})}
function base(){var b={s:i,u:url(),r:d.referrer,w:screen.width,tz:(Intl.DateTimeFormat().resolvedOptions().timeZone||''),lang:(navigator.language||''),title:(d.title||'').slice(0,500),id:id};if(tag)b.tag=tag;return b}
function p(n,x){if(n==='pageview'){var u=location.href;if(u===l)return;l=u}var b=base();b.n=n;if(x)b.x=x;post(b)}
function track(n,x){if(!n||n==='pageview')return;p(String(n).slice(0,50),x||null)}
var auto=attr('auto-track','true')!=='false';
var hst=history,ps=hst.pushState;hst.pushState=function(){ps.apply(hst,arguments);if(auto&&attr('auto-pageview','true')!=='false'){l=null;setTimeout(function(){p('pageview')},0)}};
addEventListener('popstate',function(){if(auto&&attr('auto-pageview','true')!=='false')p('pageview')});
if(auto){d.addEventListener('click',function(ev){var t=ev.target&&ev.target.closest?ev.target.closest('[data-umago-event],[data-umami-event]'):null;if(!t)return;var n=t.getAttribute('data-umago-event');if(n==null)n=t.getAttribute('data-umami-event');if(!n)return;var x={},as=t.attributes;for(var j=0;j<as.length;j++){var a=as[j];if(a.name.indexOf('data-umago-event-')===0&&a.name.length>17)x[a.name.slice(17)]=a.value;else if(a.name.indexOf('data-umami-event-')===0&&a.name.length>17&&!(a.name.slice(17) in x))x[a.name.slice(17)]=a.value}track(n,x)},true)}
var g=window.umago=function(n,x){track(n,x)};
g.identify=function(v,x){id=String(v).slice(0,50);var b=base();b.n='identify';if(x)b.x=x;post(b)};
g.attribution=function(){try{return JSON.parse(localStorage.getItem('umago_attr'))}catch(z){return null}};
if(opt('attribution')){try{var a=g.attribution();if(!a||a.t<Date.now()-7776e6)localStorage.setItem('umago_attr',JSON.stringify({r:d.referrer,l:location.href,t:Date.now()}))}catch(z){}}
if(opt('performance')&&window.PerformanceObserver){var m={};try{var o=new PerformanceObserver(function(es){es.getEntries().forEach(function(en){if(en.entryType==='largest-contentful-paint')m.lcp=Math.round(en.startTime);else if(en.entryType==='layout-shift'&&!en.hadRecentInput)m.cls=Math.round((m.cls||0)+en.value*1000)/1000;else if(en.entryType==='event'&&en.duration)m.inp=Math.max(m.inp||0,Math.round(en.duration))})});o.observe({type:'largest-contentful-paint',buffered:true});o.observe({type:'layout-shift',buffered:true});o.observe({type:'event',buffered:true,durationThreshold:40})}catch(_){}
function vit(){try{var ns=performance.getEntriesByType('navigation');if(ns&&ns[0]){var n=ns[0];if(n.responseStart>0)m.ttfb=Math.round(n.responseStart);var ps=performance.getEntriesByName('first-contentful-paint');if(ps&&ps[0])m.fcp=Math.round(ps[0].startTime)}var b=base();b.n='vitals';for(var k in m)b[k]=m[k];post(b)}catch(_){}}
var sent=false;function flush(){if(sent)return;sent=true;try{o.disconnect()}catch(_){}vit()}addEventListener('pagehide',flush);d.addEventListener('visibilitychange',function(){if(d.visibilityState==='hidden')flush()});setTimeout(flush,15000)}
if(auto&&attr('auto-pageview','true')!=='false')p('pageview')})();
