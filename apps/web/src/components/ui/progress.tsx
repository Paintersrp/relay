export function Progress({value=0}:{value?:number}){return <div className="h-2 rounded-full bg-slate-800"><div className="h-2 rounded-full bg-cyan-400" style={{width:`${value}%`}} /></div>}
