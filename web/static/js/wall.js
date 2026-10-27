/* Događanja: posters on a wall. toss, drag, tidy.
   Same as the art table snippet, but posters are taller than wide, so width (w) and height (h) are kept apart. */
const table=document.querySelector('.table'),pieces=[...table.querySelectorAll('.piece')];
let mode='pile',z=pieces.length;
const rnd=(a,b)=>a+Math.random()*(b-a),clamp=(v,a,b)=>Math.min(b,Math.max(a,v));
const scatterH=W=>Math.round(clamp(W*.66,380,720));
const metrics=()=>{const w=pieces[0].offsetWidth,h=pieces[0].offsetHeight,W=table.clientWidth;return{w,h,W,m:Math.round(w*.1),H:parseFloat(table.style.height)||scatterH(W)}};
const paint=p=>{p.style.setProperty('--x',p._x+'px');p.style.setProperty('--y',p._y+'px');p.style.setProperty('--rot',p._r+'deg')};
function stagger(){
  pieces.forEach((p,i)=>p.style.transitionDelay=i*25+'ms');
  clearTimeout(stagger.t);
  stagger.t=setTimeout(()=>pieces.forEach(p=>p.style.transitionDelay=''),pieces.length*25+900);
}
function layout(){
  const{w,h,W,m}=metrics(),g=Math.round(w*.1);let H=Math.max(scatterH(W),h+2*m);
  if(mode==='tidy'){
    const cols=Math.max(1,Math.floor((W-2*m+g)/(w+g))),rows=Math.ceil(pieces.length/cols),off=(W-(cols*(w+g)-g))/2;
    H=rows*(h+g)-g+2*m;
    pieces.forEach((p,i)=>{p._x=off+(i%cols)*(w+g);p._y=m+Math.floor(i/cols)*(h+g);p._r=0});
  }else pieces.forEach(p=>{p._x=m+p._fx*(W-w-2*m);p._y=m+p._fy*(H-h-2*m)});
  table.style.height=H+'px';
  pieces.forEach(paint);
}
function scatter(){
  mode='scatter';
  const{W}=metrics(),H=scatterH(W),n=pieces.length,cols=Math.max(2,Math.round(Math.sqrt(n*W/H))),rows=Math.max(2,Math.ceil(n/cols));
  const cells=[...pieces.keys()].sort(()=>Math.random()-.5);
  pieces.forEach((p,i)=>{
    const c=cells[i];
    p._fx=clamp(c%cols/(cols-1)+rnd(-.4,.4)/(cols-1),0,1);
    p._fy=clamp(Math.floor(c/cols)/(rows-1)+rnd(-.4,.4)/(rows-1),0,1);
    p._r=rnd(-14,14);
  });
  stagger();layout();
}
pieces.forEach((p,i)=>{p._fx=p._fy=.5;p._r=rnd(-8,8);p.style.zIndex=i});
layout();table.offsetWidth;table.classList.add('ready');
new IntersectionObserver((e,o)=>{if(e[0].isIntersecting){scatter();o.disconnect()}},{threshold:.4}).observe(table);
let lw=table.clientWidth;
new ResizeObserver(()=>{if(table.clientWidth!==lw){lw=table.clientWidth;layout()}}).observe(table);
document.getElementById('shuffle').addEventListener('click',scatter);
document.getElementById('tidy').addEventListener('click',()=>{mode='tidy';stagger();layout()});

pieces.forEach(p=>p.addEventListener('pointerdown',e=>{
  if(e.target.closest('button,a'))return;
  e.preventDefault();
  p.setPointerCapture(e.pointerId);
  p.classList.add('drag');p.style.zIndex=++z;
  const dx=e.clientX-p._x,dy=e.clientY-p._y;
  const move=ev=>{const{w,h,W,H,m}=metrics();p._x=clamp(ev.clientX-dx,m,W-w-m);p._y=clamp(ev.clientY-dy,m,H-h-m);paint(p)};
  const up=()=>{
    p.removeEventListener('pointermove',move);p.removeEventListener('pointerup',up);p.removeEventListener('pointercancel',up);
    p.classList.remove('drag');
    const{w,h,W,H,m}=metrics();
    p._fx=(p._x-m)/(W-w-2*m);p._fy=(p._y-m)/(H-h-2*m);p._r=mode==='tidy'?0:rnd(-12,12);paint(p);
  };
  p.addEventListener('pointermove',move);p.addEventListener('pointerup',up);p.addEventListener('pointercancel',up);
}));
