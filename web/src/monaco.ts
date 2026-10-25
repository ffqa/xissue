// 从本地打包加载 Monaco（部署环境可能无外网，不走 CDN）
import { loader } from '@monaco-editor/react';
import * as monaco from 'monaco-editor/esm/vs/editor/editor.api';
import 'monaco-editor/esm/vs/basic-languages/monaco.contribution';
import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker';

// Only the generic editor worker is needed for a read-only diff view. Loading
// the TS/CSS/HTML/JSON language services costs several MB and they are not used.
self.MonacoEnvironment = {
  getWorker() {
    return new editorWorker();
  },
};

loader.config({ monaco });
