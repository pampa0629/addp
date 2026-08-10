import { JupyterFrontEnd, JupyterFrontEndPlugin } from '@jupyterlab/application';
import { INotebookTracker, NotebookActions } from '@jupyterlab/notebook';

const INSERT_MESSAGE = 'addp:notebook:insert-cell';
const RESULT_MESSAGE = 'addp:notebook:insert-cell:result';

function currentSessionId(): string | null {
  const match = window.location.pathname.match(/\/notebook-sessions\/([^/]+)\//);
  return match ? decodeURIComponent(match[1]) : null;
}

const plugin: JupyterFrontEndPlugin<void> = {
  id: '@addp/notebook-bridge:plugin',
  autoStart: true,
  requires: [INotebookTracker],
  activate: (_app: JupyterFrontEnd, notebooks: INotebookTracker): void => {
    window.addEventListener('message', event => {
      if (event.origin !== window.location.origin || event.source !== window.parent) {
        return;
      }
      const request = event.data;
      if (
        !request ||
        request.type !== INSERT_MESSAGE ||
        typeof request.requestId !== 'string' ||
        typeof request.code !== 'string' ||
        request.sessionId !== currentSessionId()
      ) {
        return;
      }

      try {
        const notebook = notebooks.currentWidget?.content;
        if (!notebook) {
          throw new Error('No active Notebook is available');
        }
        NotebookActions.insertBelow(notebook);
        NotebookActions.changeCellType(notebook, 'code');
        const cell = notebook.activeCell;
        if (!cell) {
          throw new Error('The inserted code cell is unavailable');
        }
        cell.model.sharedModel.setSource(request.code);
        cell.editor?.focus();
        window.parent.postMessage(
          { type: RESULT_MESSAGE, requestId: request.requestId, ok: true },
          event.origin,
        );
      } catch (error) {
        window.parent.postMessage(
          {
            type: RESULT_MESSAGE,
            requestId: request.requestId,
            ok: false,
            error: error instanceof Error ? error.message : String(error),
          },
          event.origin,
        );
      }
    });
  },
};

export default plugin;
