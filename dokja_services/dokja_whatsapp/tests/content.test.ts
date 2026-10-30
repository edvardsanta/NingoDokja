import assert from "node:assert/strict";
import test from "node:test";
import { createWhatsAppMessageHandler, signed, validatedReplyMedia, type WhatsAppMessageContext } from "../messages/message_handler.js";
import { readDocumentStream } from "../runtime/document_download.js";
import { whatsappMessage } from "../messages/locales.js";
import { toMessageContext, type InboundMessage } from "../runtime/inbound.js";
import { RecentKeys } from "../messages/recent_keys.js";
import { dispatchToOrchestratorResponse, buildWhatsAppBridgeRequest } from "../transport/bridge.js";
import pt from "../messages/locales/pt-BR.json" with {type:"json"};
import en from "../messages/locales/en.json" with {type:"json"};

const config={orchestratorEndpoint:"http://orchestrator.test/orchestrator",orchestratorTimeoutMs:1000,selfChat:true,allowedNumbers:[],allowedGroups:[]};
function context(overrides:Partial<WhatsAppMessageContext>={}):WhatsAppMessageContext {
 return {text:"",messageId:"document-message",chatId:"self-chat",chatKind:"direct",senderId:"owner",senderNumbers:[],fromMe:true,isSelfChat:true,sentByBot:false,mentionsBot:false,timestampMs:Date.now(),reply:async()=>{},typing:async()=>{},...overrides};
}
for(const locale of ["pt-BR","en"]) {
 test(`documents are downloaded only after the orchestrator requests bytes (${locale})`,async()=>{
  const calls:any[]=[];const replies:string[]=[];let downloads=0;
  const handler=createWhatsAppMessageHandler({...config,locale},{dispatch:async(_endpoint,_timeout,body)=>{
   calls.push(structuredClone(body));return calls.length===1?{reply:"",document_required:true}:{reply:locale==="en"?"A concise summary.":"Um resumo conciso."};
  }});
  await handler(context({document:{filename:"memo.pdf",content_type:"application/pdf",size:3},downloadDocument:async()=>{downloads++;return Buffer.from("pdf");},reply:async text=>replies.push(text)}));
  assert.equal(downloads,1);assert.equal(calls.length,2);
  assert.equal(calls[0].payload.document.data_b64,undefined);
  assert.equal(calls[1].payload.document.data_b64,Buffer.from("pdf").toString("base64"));
  assert.equal(calls[1].payload.locale,locale);
  assert.ok(replies[0]!.endsWith(locale==="en"?"Signed _Ningo Dokja_":"Assinado _Ningo Dokja_"));
 });
 test(`document and publication errors use the configured catalog (${locale})`,async()=>{
  for(const reason of ["document_unsupported","post_login_required","reader_unavailable"]) {
   const replies:string[]=[];
   const handler=createWhatsAppMessageHandler({...config,locale},{dispatch:async()=>({reply:"",content_error:reason})});
   await handler(context({text:"https://example.com/post",reply:async text=>replies.push(text)}));
   assert.equal(replies[0],signed(whatsappMessage(locale,reason),locale));
  }
 });
}

test("denied, duplicate, stale and bot messages never download documents",async()=>{
 let downloads=0;let calls=0;
 const handler=createWhatsAppMessageHandler(config,{dispatch:async()=>{calls++;return {reply:""};}});
 const doc=context({document:{filename:"memo.pdf",content_type:"application/pdf",size:3},downloadDocument:async()=>{downloads++;return Buffer.from("pdf");}});
 await handler(doc);await handler(doc);
 await handler({...doc,messageId:"stale",timestampMs:1});await handler({...doc,messageId:"echo",sentByBot:true});
 assert.equal(downloads,0);assert.equal(calls,1);
});

test("publication media is validated, sent with the original caption and signed",async()=>{
 const sent:any[]=[];const replies:string[]=[];
 const media=[{filename:"post-1.mp4",content_type:"video/mp4",data_b64:Buffer.from("video").toString("base64")}];
 const handler=createWhatsAppMessageHandler({...config,locale:"en"},{dispatch:async()=>({reply:"Original caption\nhttps://example.com/post",attachments:media})});
 await handler(context({text:"https://example.com/post",reply:async text=>replies.push(text),replyMedia:async(file,caption)=>sent.push({file,caption})}));
 assert.equal(sent.length,1);assert.deepEqual(sent[0].file,media[0]);assert.match(sent[0].caption,/Original caption/);assert.match(sent[0].caption,/Signed _Ningo Dokja_/);assert.equal(replies.length,0);
});

test("unsafe media and failed deliveries produce a localized failure",async()=>{
 assert.throws(()=>validatedReplyMedia([{filename:"x",content_type:"application/javascript",data_b64:"eA=="}]));
 assert.throws(()=>validatedReplyMedia([{filename:"x",content_type:"video/mp4",data_b64:"not base64"}]));
 const replies:string[]=[];
 const handler=createWhatsAppMessageHandler(config,{dispatch:async()=>({reply:"caption",attachments:[{filename:"x",content_type:"video/mp4",data_b64:"eA=="}]})});
 await handler(context({text:"https://example.com/post",replyMedia:async()=>{throw new Error("private transport detail");},reply:async text=>replies.push(text)}));
 assert.equal(replies[0],signed(whatsappMessage("pt-BR","media_delivery_failed")));
});

test("document captions, wrappers and lazy download survive inbound normalization",async()=>{
 const sent=new RecentKeys();let downloaded=0;const outgoing:any[]=[];
 const message:InboundMessage={key:{id:"incoming",remoteJid:"1234567@s.whatsapp.net",fromMe:true},message:{documentWithCaptionMessage:{message:{documentMessage:{fileName:"memo.pdf",mimetype:"application/pdf",fileLength:{toNumber:()=>3},caption:"Summarize the conclusions"}}}}};
 const ctx=await toMessageContext(message,{ownJids:()=>["1234567@s.whatsapp.net"],sent,newMessageId:()=>"outgoing",send:async()=>{},presence:async()=>{},downloadDocument:async(raw,limit)=>{assert.equal(raw,message);assert.equal(limit,10*1024*1024);downloaded++;return Buffer.from("pdf");},sendMedia:async(_chat,media,caption,options)=>{outgoing.push({media,caption,options});return {key:{id:"sent-media"}};}});
 assert.equal(ctx.text,"Summarize the conclusions");assert.equal(ctx.document?.filename,"memo.pdf");assert.equal(ctx.document?.size,3);assert.equal(downloaded,0);
 await ctx.downloadDocument!();assert.equal(downloaded,1);
 await ctx.replyMedia!({filename:"x",content_type:"image/png",data_b64:"eA=="},"caption");
 assert.ok(sent.has("outgoing"));assert.ok(sent.has("sent-media"));assert.equal(outgoing[0].options.quoted,message);
});

test("bridge preserves structured replies while catalog keys match across languages",async()=>{
 assert.deepEqual(Object.keys(pt).sort(),Object.keys(en).sort());
 const body=buildWhatsAppBridgeRequest({content:"",document:{filename:"memo.pdf",content_type:"application/pdf",size:3},locale:"en",userId:"owner",chatId:"self",messageId:"m",chatKind:"direct",senderNumbers:[],isSelfChat:true,mentionsBot:false});
 const reply=await dispatchToOrchestratorResponse("http://orchestrator.test",1000,body,async()=>new Response(JSON.stringify({status:"ok",reply:"",document_required:true})));
 assert.equal(reply.document_required,true);assert.equal(body.event_type,"message.created");assert.equal(body.payload.locale,"en");
});

test("decrypted document streams stop at the byte limit and are always closed",async()=>{
 let closed=false;
 const stream={async *[Symbol.asyncIterator](){yield Buffer.from("abc");yield Buffer.from("def");},destroy(){closed=true;}};
 await assert.rejects(readDocumentStream(stream,5),(error:any)=>error.code==="document_too_large");
 assert.equal(closed,true);
 const accepted={async *[Symbol.asyncIterator](){yield Buffer.from("abc");},destroy(){}};
 assert.equal((await readDocumentStream(accepted,5)).toString(),"abc");
});

test("partial summaries and long post captions retain localized context",async()=>{
 const replies:string[]=[];
 const summary=createWhatsAppMessageHandler({...config,locale:"en"},{dispatch:async()=>({reply:"Summary.",summary_partial:true})});
 await summary(context({text:"summarize",reply:async text=>replies.push(text)}));
 assert.ok(replies[0]!.includes(whatsappMessage("en","summary_partial")));
 const media:any[]=[];const text:string[]=[];const original="x".repeat(1500)+" https://example.com/post";
 const posts=createWhatsAppMessageHandler({...config,locale:"en"},{dispatch:async()=>({reply:original,attachments:[{filename:"x",content_type:"video/mp4",data_b64:"eA=="}]})});
 await posts(context({text:"https://example.com/post",replyMedia:async(_,caption)=>media.push(caption),reply:async message=>text.push(message)}));
 assert.ok(media[0].length<=1000);assert.ok(text[0]!.includes("https://example.com/post"));
});
