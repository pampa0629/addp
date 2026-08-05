<template>
  <div ref="editorContainer" class="monaco-editor-container"></div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, watch } from 'vue'
import * as monaco from 'monaco-editor/esm/vs/editor/editor.api'

// 配置 Monaco Editor worker 路径
if (typeof window !== 'undefined') {
  window.MonacoEnvironment = {
    getWorkerUrl: function (moduleId, label) {
      if (label === 'json') {
        return new URL('monaco-editor/esm/vs/language/json/json.worker.js', import.meta.url).href
      }
      if (label === 'css' || label === 'scss' || label === 'less') {
        return new URL('monaco-editor/esm/vs/language/css/css.worker.js', import.meta.url).href
      }
      if (label === 'html' || label === 'handlebars' || label === 'razor') {
        return new URL('monaco-editor/esm/vs/language/html/html.worker.js', import.meta.url).href
      }
      if (label === 'typescript' || label === 'javascript') {
        return new URL('monaco-editor/esm/vs/language/typescript/ts.worker.js', import.meta.url).href
      }
      return new URL('monaco-editor/esm/vs/editor/editor.worker.js', import.meta.url).href
    }
  }
}

const props = defineProps({
  modelValue: {
    type: String,
    default: ''
  },
  language: {
    type: String,
    default: 'sql'
  },
  theme: {
    type: String,
    default: 'vs-dark'
  },
  readOnly: {
    type: Boolean,
    default: false
  },
  completions: {
    type: Array,
    default: () => []
  }
})

const emit = defineEmits(['update:modelValue', 'execute', 'selection-change'])

const editorContainer = ref(null)
let editor = null
let completionProvider = null

const registerCompletionProvider = () => {
  completionProvider?.dispose()
  const language = props.language || 'plaintext'
  completionProvider = monaco.languages.registerCompletionItemProvider(language, {
    provideCompletionItems: () => ({
      suggestions: props.completions.map(item => ({
        label: item.label || item.insertText,
        insertText: item.insertText,
        detail: item.detail || '',
        kind: monaco.languages.CompletionItemKind.Reference
      }))
    })
  })
}

onMounted(() => {
  // 注册 SQL 语言（如果尚未注册）
  const languages = monaco.languages.getLanguages()
  if (!languages.find(lang => lang.id === 'sql')) {
    monaco.languages.register({ id: 'sql' })

    // 配置 SQL 语言特性
    monaco.languages.setLanguageConfiguration('sql', {
      comments: {
        lineComment: '--',
        blockComment: ['/*', '*/']
      },
      brackets: [
        ['(', ')'],
        ['[', ']']
      ],
      autoClosingPairs: [
        { open: '(', close: ')' },
        { open: '[', close: ']' },
        { open: "'", close: "'" },
        { open: '"', close: '"' }
      ]
    })

    // 设置 SQL 关键字高亮
    monaco.languages.setMonarchTokensProvider('sql', {
      keywords: [
        'SELECT', 'FROM', 'WHERE', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'DROP',
        'ALTER', 'TABLE', 'INDEX', 'VIEW', 'JOIN', 'LEFT', 'RIGHT', 'INNER', 'OUTER',
        'ON', 'AS', 'AND', 'OR', 'NOT', 'IN', 'EXISTS', 'BETWEEN', 'LIKE', 'IS', 'NULL',
        'ORDER', 'BY', 'GROUP', 'HAVING', 'LIMIT', 'OFFSET', 'UNION', 'DISTINCT'
      ],
      operators: ['=', '>', '<', '!', '~', '?', ':', '==', '<=', '>=', '!=', '&&', '||', '++', '--', '+', '-', '*', '/', '&', '|', '^', '%'],
      symbols: /[=><!~?:&|+\-*\/\^%]+/,
      tokenizer: {
        root: [
          [/[a-z_$][\w$]*/, {
            cases: {
              '@keywords': 'keyword',
              '@default': 'identifier'
            }
          }],
          [/[A-Z][\w$]*/, {
            cases: {
              '@keywords': 'keyword',
              '@default': 'type.identifier'
            }
          }],
          { include: '@whitespace' },
          [/[{}()\[\]]/, '@brackets'],
          [/@symbols/, {
            cases: {
              '@operators': 'operator',
              '@default': ''
            }
          }],
          [/\d*\.\d+([eE][\-+]?\d+)?/, 'number.float'],
          [/\d+/, 'number'],
          [/[;,.]/, 'delimiter'],
          [/'([^'\\]|\\.)*$/, 'string.invalid'],
          [/'/, 'string', '@string'],
          [/"([^"\\]|\\.)*$/, 'string.invalid'],
          [/"/, 'string', '@stringDouble']
        ],
        whitespace: [
          [/[ \t\r\n]+/, 'white'],
          [/--.*$/, 'comment'],
          [/\/\*/, 'comment', '@comment']
        ],
        comment: [
          [/[^\/*]+/, 'comment'],
          [/\*\//, 'comment', '@pop'],
          [/[\/*]/, 'comment']
        ],
        string: [
          [/[^\\']+/, 'string'],
          [/\\./, 'string.escape'],
          [/'/, 'string', '@pop']
        ],
        stringDouble: [
          [/[^\\"]+/, 'string'],
          [/\\./, 'string.escape'],
          [/"/, 'string', '@pop']
        ]
      }
    })
  }

  if (!monaco.languages.getLanguages().find(lang => lang.id === 'cypher')) {
    monaco.languages.register({ id: 'cypher' })
    monaco.languages.setLanguageConfiguration('cypher', {
      comments: { lineComment: '//' },
      brackets: [['(', ')'], ['[', ']'], ['{', '}']],
      autoClosingPairs: [
        { open: '(', close: ')' }, { open: '[', close: ']' }, { open: '{', close: '}' },
        { open: "'", close: "'" }, { open: '"', close: '"' }
      ]
    })
  }

  // 创建编辑器
  editor = monaco.editor.create(editorContainer.value, {
    value: props.modelValue,
    language: props.language,
    theme: props.theme,
    readOnly: props.readOnly,
    automaticLayout: true,
    fontSize: 14,
    minimap: { enabled: false },
    scrollBeyondLastLine: false,
    lineNumbers: 'on',
    renderWhitespace: 'selection',
    tabSize: 2,
    wordWrap: 'on'
  })

  // 监听内容变化
  editor.onDidChangeModelContent(() => {
    emit('update:modelValue', editor.getValue())
  })

  editor.onDidChangeCursorSelection(() => {
    const selection = editor.getSelection()
    emit('selection-change', editor.getModel()?.getValueInRange(selection) || '')
  })

  // 添加执行快捷键 (Ctrl+Enter 或 Cmd+Enter)
  editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter, () => {
    emit('execute')
  })

  registerCompletionProvider()
})

onUnmounted(() => {
  if (editor) {
    editor.dispose()
  }
  completionProvider?.dispose()
})

// 监听外部 value 变化
watch(() => props.modelValue, (newValue) => {
  if (editor && editor.getValue() !== newValue) {
    editor.setValue(newValue)
  }
})

watch(() => props.language, (language) => {
  if (editor?.getModel()) {
    monaco.editor.setModelLanguage(editor.getModel(), language || 'plaintext')
    registerCompletionProvider()
  }
})

// 暴露方法给父组件
defineExpose({
  focus: () => editor?.focus(),
  getValue: () => editor?.getValue(),
  setValue: (value) => editor?.setValue(value),
  insertText: (value) => {
    if (!editor || !value) return
    const selection = editor.getSelection()
    editor.executeEdits('catalog', [{ range: selection, text: value, forceMoveMarkers: true }])
    editor.focus()
  },
  getSelection: () => {
    const selection = editor?.getSelection()
    return editor?.getModel()?.getValueInRange(selection) || ''
  },
  setLanguage: (lang) => {
    if (editor) {
      monaco.editor.setModelLanguage(editor.getModel(), lang)
    }
  }
})
</script>

<style scoped>
.monaco-editor-container {
  width: 100%;
  height: 100%;
  min-height: 200px;
}
</style>
