import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import test from 'node:test';
const source = readFileSync(new URL('../src/views/PoolView.vue', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
const handler = source.match(/async function submitBulkRotate\(\) \{[\s\S]*?\n\}/)[0];
function setup() {
 const calls=[], alerts=[], messages=[];
 const state={bulkRotateSaving:{value:false},bulkRotateOpen:{value:true},bulkRotateOption:{value:60},bulkRotateCustom:{value:30},memberSelected:{value:[{alias_id:1,address:'active@test',enabled:true,state:'used'},{alias_id:2,address:'legacy@test',enabled:true,state:'used'},{alias_id:3,address:'failed@test',enabled:true,state:'used'},{alias_id:4,address:'idle@test',enabled:true,state:'available'}]},auth:{state:{csrfToken:'csrf'}},
 async fetchMemberAlias(id){return {id,credentialMode:id===2?'legacy':'v2',autoRotateDeadline:id===1?'existing':null}},
 async setAliasAutoRotate(id,minutes,csrf){calls.push({id,minutes,csrf});if(id===3)throw new Error('network error')},
 ElMessage:{error:m=>messages.push(m),success:m=>messages.push(m)},ElMessageBox:{async confirm(m){alerts.push(m);return 'confirm'},async alert(m){alerts.push(m)}}};
 vm.createContext(state);vm.runInContext(handler,state);return {state,calls,alerts,messages};
}
test('active rules are submitted and all failures are reported',async()=>{const {state,calls,alerts}=setup();await state.submitBulkRotate();assert.deepEqual(calls,[{id:1,minutes:60,csrf:'csrf'},{id:3,minutes:60,csrf:'csrf'}]);assert.match(alerts[0],/清空已有倒计时/);assert.match(alerts[1],/成功 1 个，失败 2 个/);assert.match(alerts[1],/legacy@test/);assert.match(alerts[1],/failed@test/);assert.equal(state.bulkRotateSaving.value,false);assert.equal(state.bulkRotateOpen.value,false)});
test('cancel and invalid duration make no changes; disable sends zero',async()=>{const {state,calls,messages}=setup();state.ElMessageBox.confirm=async()=>{throw Error('cancel')};await state.submitBulkRotate();assert.equal(calls.length,0);assert.equal(state.bulkRotateSaving.value,false);state.bulkRotateOption.value='custom';state.bulkRotateCustom.value=0;await state.submitBulkRotate();assert.equal(calls.length,0);assert.equal(messages.length,1);state.bulkRotateOption.value=0;state.ElMessageBox.confirm=async()=>'confirm';await state.submitBulkRotate();assert.equal(calls[0].minutes,0)});
