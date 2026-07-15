// Native drag-out: начать OS-перетаскивание реальных файлов, чтобы их можно
// было бросить в DAW, папку проводника и т.д. HTML5 drag-and-drop не умеет
// отдавать файлы наружу, поэтому используется tauri-plugin-drag (OLE
// DoDragDrop на Windows). Плагин зарегистрирован в src-tauri; команда
// вызывается напрямую через invoke — отдельный npm-пакет не нужен.
//
// Паттерн использования (рекомендован самим плагином): элемент помечается
// draggable, в dragstart отменяем HTML5-драг и запускаем нативный.

import { isTauri } from "./tauri";

// Превью у курсора при перетаскивании — иконка приложения (128x128.png).
const DRAG_ICON =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAIAAAACACAYAAADDPmHLAAADu0lEQVR4nO2cPW7bQBBGScOF4d6AC58jZZocIJfIIXKEHCKXyAHSpMw5VAhQb7gw4BSGHP2QlJacGc7u915lGRApzvd2drVYqO8ceXp8ePO8vgqb7a73urbphQk8BkshTC5E8OtgIcKiCxB8DpaIMOuNBJ+TOSLclL6B8PMyJ5siAQg/P6UZXdUyCL5OrpkSLnYAwq+Xa7KbFIDw6+dShsWLQGiLUQEY/e0wleWgAITfHmOZnglA+O0ylC1rAHGOBGD0t89pxnQAcT52iqJH//3dbeTtquD55TXsXvtdwtAUCH2aw/pEydB3nf/oJ/j5eIqw2e569zUA4S/Du35uVyd4O/a19OgGLh2A8H3wqGvvMf8jgB/WXcC8AxC+L9b1NRWA8GOwrDM7geKYqVTj6P/74+vH35++/1rxk5Rzf3drsh6Q7QCH4Q+9VkFSgLGwFSUwEaDG9t8CFnWX7ADwHwQQBwHEaWby/vPty9Hrzz9/V3mPaJroAKfBjP0v+z3WoHoBpkKwCijiHmtRvQCwDAQQBwHEQQBxEEAcBBAHAcRJuxNY82GNQ7I/R8oO0MphjRqeI50ArRzWqOU50gkAsSCAOAggDgKIgwDiIIA4IRtBh4cmWjhG5U1kvdw7wOmJmdpP0HgTXS9XAcY+PBIMs0a9WAOIgwDiIIA4CCAOAoiDAOIggDgIIA4CiIMA4iCAOAggDgKIgwDiIIA4CCAOAoiDAOK4CjB2oJGDocOsUS/3DnD64Ql/muh6hRwLJ/QyIuvFGkAcBBAHAcRBAHEQQBwEECedAGM/pZbxJ9amqOU50gnQdedFyla0a6nhOdL+UGTGYs0h+3Ok7AAQBwKIgwDiIIA4CCAOAoiDAOJUL8DU4QmrgxUR91iL6gXouuEQrIOJuMcapN0JLCUijBYCP6WJDgDzQQBxEEAcEwGeX14tLgOFWNRdsgPUclgjAkkBuq6OwxoR9E+PD29WF7u/a+ZbZXqspl3ZDgDvmArAYjAGyzrfbLa73uxqHRJ4Y1nfzXbXMwWI4yIAXcAHj7q6Ldv3H5ZvBsvxHFA3Xfc+F3jdgG6wDK/67TMPGZ50g3KiBs7RyLfcFLoEMpwTFfphx18tBaaGHBx9C/BcC0AOTjNmH0CcMwHoAu0ylO1gB0CC9hjLdHQKQIJ2mMqSNYA4kwLQBernUoYXOwAS1Ms12RWFG7lTCPMpGbRFawC6QX5KMypeBCJBXuZksyhMpoQcLBmUJqMZEdbBohubtnNEiMFyGnadzxHCBs911z/1Uz2iw1EcVAAAAABJRU5ErkJggg==";

/** Запускает нативное перетаскивание файлов. Молча no-op вне Tauri или без путей. */
export async function startFileDrag(paths: string[]): Promise<void> {
  const files = paths.filter(Boolean);
  if (!isTauri() || files.length === 0) return;
  try {
    const { invoke, Channel } = await import("@tauri-apps/api/core");
    await invoke("plugin:drag|start_drag", {
      item: files,
      image: DRAG_ICON,
      onEvent: new Channel(),
    });
  } catch (e) {
    console.error("startFileDrag:", e);
  }
}

/**
 * Пропсы для элемента, из которого можно утащить файлы наружу:
 * `<div {...fileDragProps(() => [path])}>`. getPaths вызывается в момент
 * начала перетаскивания — снимает актуальное выделение, а не то, что было
 * при рендере.
 */
export function fileDragProps(getPaths: () => (string | undefined)[]) {
  return {
    draggable: true,
    onDragStart: (e: { preventDefault(): void }) => {
      e.preventDefault(); // отменяем HTML5-драг — вместо него нативный OLE
      void startFileDrag(getPaths().filter((p): p is string => !!p));
    },
  };
}
