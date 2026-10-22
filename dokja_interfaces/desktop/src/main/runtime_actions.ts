import { isRecord, text } from "../shared/records.js";
import { RUNTIME_MODULES, type RuntimeModule, type RuntimeSnapshot, type RuntimeState } from "../shared/runtime.js";

export function runtimeSet(raw:Record<string,unknown>):Record<string,unknown>|undefined {
 return RUNTIME_MODULES.includes(raw.module as RuntimeModule) && typeof raw.enabled === "boolean" ? {module:raw.module,enabled:raw.enabled}:undefined;
}
export function runtimeSnapshot(body:Record<string,unknown>):RuntimeSnapshot {
 if (!Array.isArray(body.modules)||typeof body.busy!=="boolean") throw new Error("invalid runtime response");
 const states=["starting","ready","running","stopping","stopped","failed"];
 const modules:RuntimeSnapshot["modules"]=[];
 // A newer controller may manage modules this build does not know: they are left out, so the
 // controllers of every interface need not ship together.
 for (const raw of body.modules.slice(0,64)) {
  if(!isRecord(raw)||!states.includes(text(raw.state))) throw new Error("invalid runtime module");
  if(!RUNTIME_MODULES.includes(raw.name as RuntimeModule)) continue;
  modules.push({name:raw.name as RuntimeModule,state:raw.state as RuntimeState,error:text(raw.error).slice(0,400),dependencies:Array.isArray(raw.dependencies)?raw.dependencies.filter((v):v is string=>typeof v==="string").slice(0,4).map(v=>v.slice(0,40)):[]});
 }
 return {modules,busy:body.busy,error:text(body.error).slice(0,400)};
}
