import { isRecord, text } from "../shared/records.js";
import { RUNTIME_MODULES, type RuntimeModule, type RuntimeSettings, type RuntimeSnapshot, type RuntimeState } from "../shared/runtime.js";

export function runtimeModule(raw:Record<string,unknown>):Record<string,unknown>|undefined {
 return RUNTIME_MODULES.includes(raw.module as RuntimeModule) ? {module:raw.module}:undefined;
}
export function runtimeLogs(body:Record<string,unknown>):{module:RuntimeModule;text:string} {
 if (!RUNTIME_MODULES.includes(body.module as RuntimeModule) || typeof body.text !== "string") throw new Error("invalid runtime logs");
 return {module:body.module as RuntimeModule,text:body.text.slice(-24576)};
}
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
  modules.push({name:raw.name as RuntimeModule,state:raw.state as RuntimeState,error:text(raw.error).slice(0,400),progress:text(raw.progress).slice(0,100),dependencies:Array.isArray(raw.dependencies)?raw.dependencies.filter((v):v is string=>typeof v==="string").slice(0,4).map(v=>v.slice(0,40)):[]});
 }
 return {modules,busy:body.busy,error:text(body.error).slice(0,400)};
}

// A settings change names each setting by id and gives its value as text; the controller checks
// every value again. Anything else in the payload is dropped.
const SETTING_ID = /^[a-z]+\.[a-z_]+$/;
export function runtimeSettingsSet(raw:Record<string,unknown>):Record<string,unknown>|undefined {
 if(!isRecord(raw.values)) return undefined;
 const values:Record<string,string>={};
 const entries=Object.entries(raw.values);
 if(entries.length>16) return undefined;
 for (const [id,value] of entries) {
  if(!SETTING_ID.test(id)||typeof value!=="string"||value.length>2000) return undefined;
  values[id]=value;
 }
 return Object.keys(values).length>0?{values}:undefined;
}
const strings=(value:unknown,limit:number,size:number):string[]=>Array.isArray(value)?value.filter((v):v is string=>typeof v==="string").slice(0,limit).map(v=>v.slice(0,size)):[];
export function runtimeSettings(body:Record<string,unknown>):RuntimeSettings {
 if(!Array.isArray(body.settings)||typeof body.busy!=="boolean") throw new Error("invalid runtime settings");
 const settings:RuntimeSettings["settings"]=[];
 for (const raw of body.settings.slice(0,64)) {
  if(!isRecord(raw)||!SETTING_ID.test(text(raw.id))) throw new Error("invalid runtime setting");
  settings.push({id:text(raw.id),group:text(raw.group).slice(0,20),kind:text(raw.kind).slice(0,10),options:strings(raw.options,8,20),
   value:text(raw.value).slice(0,2000),affects:strings(raw.affects,12,40),note:text(raw.note).slice(0,20),default:text(raw.default).slice(0,200),readOnly:raw.read_only===true});
 }
 return {settings,webhookChannels:strings(body.webhook_channels,64,20).filter(id=>/^[0-9]+$/.test(id)),busy:body.busy,error:text(body.error).slice(0,400)};
}
