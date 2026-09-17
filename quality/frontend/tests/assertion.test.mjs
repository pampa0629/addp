import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import { ASSERTION_OPERATORS, assertionInputs, assertionBindings, defaultAssertion, validAssertionBindings } from '../src/utils/assertion.js';
import { createCheckItem, serializeCheckItems, serializeConstraint, defaultConstraint } from '../src/utils/planContract.js';

const expression = { op:'eq', args:[{op:'field',field:'actual'}, {op:'count_distinct',relation:'detail',field:'item',where:{op:'eq',args:[{op:'field',field:'key'},{op:'field',relation:'detail',field:'key'}]}}] };
test('assertion symbols bind separately from frozen constraints', () => {
  assert.deepEqual(assertionInputs(expression), {'':['actual','key'],detail:['item','key']});
  const rule = {id:23,revision_no:2,type:'relational_assertion',params:{assertion:expression}};
  const item = createCheckItem(rule,'summary','key');
  assert.deepEqual(item.bindings,{table:'summary',fields:{actual:'',key:''},relations:{detail:{table:'',fields:{item:'',key:''}}}});
  assert.equal(validAssertionBindings(expression,item.bindings,['summary','facts']),false);
  item.bindings.fields={actual:'metric_value',key:'person_id'};
  item.bindings.relations.detail={table:'facts',fields:{item:'activity_id',key:'person_id'}};
  assert.equal(validAssertionBindings(expression,item.bindings,['summary','facts']),true);
  assert.equal(validAssertionBindings(expression,item.bindings,['summary']),false);
  assert.deepEqual(serializeConstraint(rule.type,rule.params),{assertion:expression});
  assert.equal(Object.hasOwn(serializeCheckItems([item])[0],'rule'),false);
  assert.deepEqual(rule.params,{assertion:expression});
});
test('revision upgrade preserves matching bindings and drops removed symbols', () => {
  const previous = {fields:{actual:'metric_value',key:'person_id',old:'obsolete'},relations:{detail:{table:'facts',fields:{key:'person_id',item:'activity_id',old:'obsolete'}},removed:{table:'old',fields:{}}}};
  const next = assertionBindings(expression,previous);
  assert.deepEqual(next,{fields:{actual:'metric_value',key:'person_id'},relations:{detail:{table:'facts',fields:{item:'activity_id',key:'person_id'}}}});
});
test('every operator has clean defaults and both language labels', () => {
  assert.deepEqual(defaultConstraint('relational_assertion'),{assertion:defaultAssertion()});
  for(const language of ['zh-cn','en']) {
    const messages=JSON.parse(readFileSync(new URL(`../src/i18n/${language}.json`,import.meta.url),'utf8'));
    for(const op of ASSERTION_OPERATORS) {
      assert.equal(defaultAssertion(op).op,op);
      assert.ok(messages.quality.assertion[`op_${op}`],`${language} ${op}`);
    }
  }
});
test('valid logical symbols cannot collide with JavaScript object properties', () => {
  const expression = { op:'exists',relation:'constructor',where:{op:'eq',args:[{op:'field',field:'constructor'},{op:'field',relation:'constructor',field:'constructor'}]} };
  assert.deepEqual(assertionInputs(expression),{'':['constructor'],constructor:['constructor']});
  assert.deepEqual(assertionBindings(expression),{fields:{constructor:''},relations:{constructor:{table:'',fields:{constructor:''}}}});
});
test('decimal literals survive browser JSON round trips without number coercion', () => {
  const expression = { op:'eq',args:[{op:'field',field:'count'},{op:'number',value:'9007199254740993.123456789012345678'}] };
  assert.deepEqual(serializeConstraint('relational_assertion',{assertion:expression}),{assertion:expression});
});
