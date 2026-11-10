import { expect, it } from "vitest"
import { phonePrediction } from "./composer-prediction"
import type { RemoteEvent, ThreadDetail } from "./rpc"
const detail: ThreadDetail={id:"thread",title:"Task",turns:[{id:"turn",status:"done"}]}
const event=(kind:string,seq:number, extra:Partial<RemoteEvent>={}):RemoteEvent=>({thread_id:"thread",turn_id:"turn",kind,seq,text:"",created_at:"",...extra})
it("uses the latest completed primary turn and ignores another turn's late suggestion",()=>{
 const events=[event("user_message",1),event("done",2),event("composer_prediction",3,{text:"Next action"}),event("composer_prediction",4,{turn_id:"older",text:"Wrong"})]
 expect(phonePrediction(events,detail)).toEqual({turnId:"turn",text:"Next action"})
 expect(phonePrediction(events,{...detail,running:{thread_id:"thread",title:"Task"}})).toBeUndefined()
 expect(phonePrediction(events,{...detail,waiting:true})).toBeUndefined()
 expect(phonePrediction(events,detail,false)).toBeUndefined()
})
it("new input, a decline and errors remove the old suggestion",()=>{
 const events=[event("done",1),event("composer_prediction",2,{text:"Next"})]
 for (const ev of [event("user_message",3,{turn_id:"next"}),event("composer_prediction",3,{text:""}),event("error",3,{err:"failure"})]) expect(phonePrediction([...events,ev],detail)).toBeUndefined()
 expect(phonePrediction([event("composer_prediction",1,{text:"Unconfirmed"})],{id:"thread",title:"Task"})).toBeUndefined()
})
it("a fresh live turn replaces the old snapshot and history order remains stable",()=>{
 const events=[event("done",2),event("turn",3,{turn_id:"next"}),event("done",4,{turn_id:"next"}),event("composer_prediction",5,{turn_id:"next",text:"Next output"})]
 expect(phonePrediction([...events].reverse(),detail)).toEqual({turnId:"next",text:"Next output"})
 expect(phonePrediction([...events,event("composer_prediction",6,{thread_id:"another",text:"Crossed"})],detail)?.text).toBe("Next output")
})

it("does not revive a suggestion from an earlier page without the latest turn",()=>{
 const old=[event("user_message",1,{turn_id:"old"}),event("done",2,{turn_id:"old"}),event("composer_prediction",3,{turn_id:"old",text:"Old output"})]
 expect(phonePrediction(old,detail)).toBeUndefined()
})
