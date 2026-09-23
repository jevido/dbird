<script lang="ts" module>
  import { monaco, languageFor, setModelSchema, forgetModel } from './monaco';

  // One model per tab, so switching tabs keeps undo history; view state keeps
  // cursor and scroll position.
  const models = new Map<string, monaco.editor.ITextModel>();
  const viewStates = new Map<string, monaco.editor.ICodeEditorViewState | null>();

  export function forgetEditorState(tabId: string) {
    const m = models.get(tabId);
    if (m) {
      forgetModel(m);
      m.dispose();
    }
    models.delete(tabId);
    viewStates.delete(tabId);
  }

  function modelFor(tabId: string, value: string, dialect: string): monaco.editor.ITextModel {
    let m = models.get(tabId);
    if (!m || m.isDisposed()) {
      const uri = monaco.Uri.parse(`inmemory://dbird/${tabId}.sql`);
      // The model can outlive this module (e.g. after a hot reload in dev), and
      // Monaco refuses a second model with the same URI.
      m = monaco.editor.getModel(uri) ?? monaco.editor.createModel(value, languageFor[dialect] ?? 'sql', uri);
      m.setEOL(monaco.editor.EndOfLineSequence.LF);
      models.set(tabId, m);
    }
    return m;
  }
</script>

<script lang="ts">
  import { onDestroy, untrack } from 'svelte';

  interface Props {
    tabId: string;
    value: string;
    dialect: string;
    schema: Record<string, string[]> | undefined;
    fontSize: number;
    onchange: (value: string) => void;
    onrun: (script: boolean) => void;
  }

  let { tabId, value, dialect, schema, fontSize, onchange, onrun }: Props = $props();

  let editor: monaco.editor.IStandaloneCodeEditor | undefined;
  let currentTab = '';
  let applyingExternal = false;
  let flashDecorations: monaco.editor.IEditorDecorationsCollection | undefined;
  let flashTimer: ReturnType<typeof setTimeout> | undefined;

  function run(script: boolean) {
    editor?.trigger('dbird', 'hideSuggestWidget', {});
    onrun(script);
  }

  function mount(el: HTMLElement) {
    untrack(() => {
      currentTab = tabId;
      const model = modelFor(tabId, value, dialect);
      setModelSchema(model, schema);
      editor = monaco.editor.create(el, {
        model,
        theme: 'dbird-dark',
        automaticLayout: true,
        fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', 'SF Mono', Menlo, Consolas, 'DejaVu Sans Mono', monospace",
        fontSize,
        fontLigatures: false,
        lineHeight: 1.55,
        minimap: { enabled: false },
        scrollBeyondLastLine: false,
        renderLineHighlight: 'all',
        padding: { top: 6 },
        tabSize: 2,
        wordBasedSuggestions: 'off',
        suggest: { showWords: false },
        quickSuggestions: { other: true, comments: false, strings: false },
        fixedOverflowWidgets: true,
        contextmenu: true,
        stickyScroll: { enabled: false },
        smoothScrolling: true,
        cursorBlinking: 'smooth',
        occurrencesHighlight: 'off',
      });
      flashDecorations = editor.createDecorationsCollection();
      const { KeyMod, KeyCode } = monaco;
      editor.addAction({
        id: 'dbird.run',
        label: 'Execute statement at cursor',
        keybindings: [KeyMod.CtrlCmd | KeyCode.Enter],
        contextMenuGroupId: 'navigation',
        contextMenuOrder: 0,
        run: () => run(false),
      });
      editor.addAction({
        id: 'dbird.runScript',
        label: 'Execute script',
        keybindings: [KeyMod.CtrlCmd | KeyMod.Shift | KeyCode.Enter, KeyMod.Alt | KeyCode.KeyX],
        contextMenuGroupId: 'navigation',
        contextMenuOrder: 1,
        run: () => run(true),
      });
      editor.onDidChangeModelContent(() => {
        if (!applyingExternal) onchange(editor!.getValue());
      });
      const vs = viewStates.get(tabId);
      if (vs) editor.restoreViewState(vs);
      editor.focus();
    });
    return () => {
      if (editor) viewStates.set(currentTab, editor.saveViewState());
      clearTimeout(flashTimer);
      editor?.dispose(); // models are kept for when the editor is recreated
      editor = undefined;
    };
  }

  // Swap models when the active tab changes.
  $effect(() => {
    const id = tabId;
    if (!editor || id === currentTab) return;
    viewStates.set(currentTab, editor.saveViewState());
    currentTab = id;
    const model = untrack(() => modelFor(id, value, dialect));
    editor.setModel(model);
    const vs = viewStates.get(id);
    if (vs) editor.restoreViewState(vs);
    editor.focus();
  });

  // Language and completion schema follow the tab's connection.
  $effect(() => {
    const lang = languageFor[dialect] ?? 'sql';
    const s = schema;
    void tabId;
    const model = editor?.getModel();
    if (!model) return;
    if (model.getLanguageId() !== lang) monaco.editor.setModelLanguage(model, lang);
    setModelSchema(model, s);
  });

  $effect(() => {
    const size = fontSize;
    editor?.updateOptions({ fontSize: size });
  });

  // External value changes (e.g. opening a file into the tab).
  $effect(() => {
    const v = value;
    void tabId;
    const model = editor?.getModel();
    if (!model || model.getValue() === v) return;
    applyingExternal = true;
    model.pushEditOperations([], [{ range: model.getFullModelRange(), text: v }], () => null);
    applyingExternal = false;
  });

  onDestroy(() => {
    if (editor) viewStates.set(currentTab, editor.saveViewState());
  });

  // Selection as string offsets into the document (UTF-16, same as JS strings).
  export function selection() {
    const model = editor?.getModel();
    const sel = editor?.getSelection();
    const pos = editor?.getPosition();
    if (!model || !sel || !pos) return { from: 0, to: 0, head: 0 };
    return {
      from: model.getOffsetAt(sel.getStartPosition()),
      to: model.getOffsetAt(sel.getEndPosition()),
      head: model.getOffsetAt(pos),
    };
  }

  // Briefly highlights the statements that were just executed.
  export function flash(ranges: { from: number; to: number }[]) {
    const model = editor?.getModel();
    if (!model || !flashDecorations || ranges.length === 0) return;
    flashDecorations.set(
      ranges
        .filter((r) => r.to > r.from)
        .map((r) => ({
          range: monaco.Range.fromPositions(model.getPositionAt(r.from), model.getPositionAt(r.to)),
          options: { inlineClassName: 'dbird-executed' },
        })),
    );
    clearTimeout(flashTimer);
    flashTimer = setTimeout(() => flashDecorations?.clear(), 450);
  }

  export function focus() {
    editor?.focus();
  }
</script>

<div class="editor" {@attach mount}></div>

<style>
  .editor {
    height: 100%;
    min-height: 0;
    overflow: hidden;
  }
  .editor :global(.dbird-executed) {
    background: var(--flash);
  }
</style>
