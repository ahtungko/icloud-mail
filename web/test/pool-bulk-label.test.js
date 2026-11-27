import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import {readFileSync} from 'node:fs';
const source=readFileSync(new URL('../src/views/PoolView.vue',import.meta.url),'utf8').replace(/\r\n/g,'\n');
const code=source.match(/async function submitBulkLabel\(\) \{[\s\S]*?\n\}/)[0];
test('batch label updates selected addresses and reports individual failures',async()=>{
 const calls=[], alerts=[];
 const state={bulkLabelSaving:{value:false},bulkLabelOpen:{value:true},bulkLabelText:{value:' 送人了 '},memberSelected:{value:[{alias_id:1,address:'one@test'},{alias_id:2,address:'two@test'}]},request:async(path,method,body)=>{calls.push({path,method,label:body.local_note});if(path.includes('/2'))throw Error('失败')},loadMembers:async()=>{},ElMessage:{error(){},success(){}},ElMessageBox:{alert:async(msg)=>alerts.push(msg)}};
 vm.createContext(state);vm.runInContext(code,state);await state.submitBulkLabel();
 assert.deepEqual(calls,[{path:'/aliases/1',method:'PATCH',label:'送人了'},{path:'/aliases/2',method:'PATCH',label:'送人了'}]);assert.match(alerts[0],/成功 1 个，失败 1 个/);assert.match(alerts[0],/two@test/);assert.equal(state.bulkLabelSaving.value,false);
});

const loadCode = source.match(/async function loadMembers\(signal,[\s\S]*?\n\}/)[0];

function setupMemberReload({ preserveSelection = true, superseded = false } = {}) {
 const freshRows = [{ alias_id: 1, local_note: '送人了' }, { alias_id: 3 }];
 const state = {
  memberReadTicket: 0,
  memberSelected: { value: [{ alias_id: 1 }, { alias_id: 2 }] },
  members: { value: { items: [] } },
  memberState: { value: '' }, search: { value: '' }, memberPage: { value: 1 },
  request: async () => ({ items: freshRows }), query: () => '', AbortSignal,
  attempt: (fn) => fn(),
  nextTick: async () => {
   state.memberSelected.value = [];
   if (superseded) state.memberReadTicket++;
  },
  memberTable: { value: { toggleRowSelection(row, selected) {
   assert.equal(selected, true);
   state.memberSelected.value.push(row);
  } } },
 };
 vm.createContext(state);
 vm.runInContext(loadCode, state);
 return { state, freshRows, reload: () => state.loadMembers(undefined, { preserveSelection }) };
}

test('batch label requests selection preservation when refreshing', async () => {
 let options;
 const state = {
  bulkLabelSaving: { value: false }, bulkLabelOpen: { value: true },
  bulkLabelText: { value: '备注' }, memberSelected: { value: [{ alias_id: 1 }] },
  request: async () => {}, loadMembers: async (_, value) => { options = value; },
  ElMessage: { success() {} },
 };
 vm.runInNewContext(code, state);
 await state.submitBulkLabel();
 assert.equal(options.preserveSelection, true);
});

test('member reload restores selected visible rows using fresh data', async () => {
 const { state, freshRows, reload } = setupMemberReload();
 await reload();
 assert.equal(state.memberSelected.value.length, 1);
 assert.equal(state.memberSelected.value[0], freshRows[0]);
});

test('superseded member reload does not restore stale selection', async () => {
 const { state, reload } = setupMemberReload({ superseded: true });
 await reload();
 assert.equal(state.memberSelected.value.length, 0);
});
