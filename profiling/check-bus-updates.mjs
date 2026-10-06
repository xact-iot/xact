import fs from 'node:fs/promises';
const state=JSON.parse(await fs.readFile(process.argv[2],'utf8'));
const base=state.url.replace(/\/$/,'');
const login=await fetch(base+'/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:'admin',password:state.password})});
if(!login.ok) throw new Error('Test login failed');
const auth=await login.json();
async function sample(){
 const qs=new URLSearchParams({depth:'-1'});
 for(const suffix of ['*.meta.lat','*.meta.lon','*.meta.ts']) qs.append('select',suffix);
 const r=await fetch(base+'/api/v1/nodes/default/PUBLIC_BUS/BUSES?'+qs,{headers:{Authorization:'Bearer '+auth.token}});
 if(!r.ok) throw new Error('Projection read failed '+r.status);
 const json=await r.json();
 const leaves=new Map();
 const walk=(node,path)=>{if(node.type==='leaf') leaves.set(path,{value:node.value,timestamp:node.timestamp});for(const child of node.children||[])walk(child,path+'.'+child.name)};
 walk(json,'BUSES');
 return leaves;
}
let previous=await sample();
const records=[];
for(let i=0;i<3;i++){
 await new Promise(r=>setTimeout(r,30000));
 const current=await sample();
 let changedCoordinates=0,newCoordinates=0,changedTimestamps=0;
 for(const [key,value] of current){if(!key.endsWith('.lat')&&!key.endsWith('.lon'))continue;const before=previous.get(key);if(!before)newCoordinates++;else{if(before.value!==value.value)changedCoordinates++;if(before.timestamp!==value.timestamp)changedTimestamps++}}
 records.push({sample:i+1,coordinateTags:[...current.keys()].filter(k=>k.endsWith('.lat')||k.endsWith('.lon')).length,changedCoordinates,newCoordinates,changedTimestamps});
 console.log(JSON.stringify(records.at(-1)));
 await fs.writeFile(state.output+'/map-data-updates.json',JSON.stringify(records,null,2));
 previous=current;
}
if(!records.some(r=>r.changedCoordinates>0||r.changedTimestamps>0))throw new Error('No repeated bus coordinate updates observed');
