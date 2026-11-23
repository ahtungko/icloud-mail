import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import {readFileSync} from 'node:fs';
const source=readFileSync(new URL('../src/views/PoolView.vue',import.meta.url),'utf8').replace(/\r\n/g,'\n');
const code=source.match(/async function submitBulkLabel\(\) \{[\s\S]*?\n\}/)[0];
test('batch label updates selected addresses and reports individual failures',async()=>{
 const calls=[], alerts=[];
 const state={bulkLabelSaving:{value:false},bulkLabelOpen:{value:true},bulkLabelText:{value:' 送人了 '},memberSelected:{value:[{alias_id:1,address:'one@test'},{alias_id:2,address:'two@test'}]},request:async(path,method,body)=>{calls.push({path,method,label:body.label});if(path.includes('/2'))throw Error('失败')},loadMembers:async()=>{},ElMessage:{error(){},success(){}},ElMessageBox:{alert:async(msg)=>alerts.push(msg)}};
 vm.createContext(state);vm.runInContext(code,state);await state.submitBulkLabel();
 assert.deepEqual(calls,[{path:'/aliases/1',method:'PATCH',label:'送人了'},{path:'/aliases/2',method:'PATCH',label:'送人了'}]);assert.match(alerts[0],/成功 1 个，失败 1 个/);assert.match(alerts[0],/two@test/);assert.equal(state.bulkLabelSaving.value,false);
});
