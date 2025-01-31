package com.sourcegraph.cody.completions;

import com.intellij.openapi.actionSystem.DataContext;
import com.intellij.openapi.command.WriteCommandAction;
import com.intellij.openapi.editor.Caret;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.editor.Inlay;
import com.intellij.openapi.editor.ScrollType;
import com.intellij.openapi.editor.actionSystem.EditorAction;
import com.intellij.openapi.editor.actionSystem.EditorActionHandler;
import java.util.List;
import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;

/**
 * The action that gets triggered when the user accepts a Cody completion.
 *
 * <p>The action works by reading the Inlay at the caret position and inserting the completion text
 * into the editor.
 */
public class AcceptCodyCompletionAction extends EditorAction {
  public AcceptCodyCompletionAction() {
    super(new AcceptCompletionActionHandler());
  }

  private static class AcceptCompletionActionHandler extends EditorActionHandler {

    @Override
    protected boolean isEnabledForCaret(
        @NotNull Editor editor, @NotNull Caret caret, DataContext dataContext) {
      // Returns false to fall back to normal TAB character if there is no suggestion at the caret.
      return getCodyCompletionAtCaret(editor, caret) != null;
    }

    private @Nullable CodyCompletionElementRenderer getCodyCompletionAtCaret(
        @NotNull Editor editor, @Nullable Caret caret) {
      if (caret == null) {
        return null;
      }
      Inlay<?> inlay = editor.getInlayModel().getInlineElementAt(caret.getVisualPosition());
      if (inlay != null && inlay.getRenderer() instanceof CodyCompletionElementRenderer) {
        return (CodyCompletionElementRenderer) inlay.getRenderer();
      }
      return null;
    }

    @Override
    protected void doExecute(
        @NotNull Editor editor, @Nullable Caret maybeCaret, DataContext dataContext) {

      if (maybeCaret == null) {
        List<Caret> carets = editor.getCaretModel().getAllCarets();
        if (carets.size() != 1) {
          // Only accept completion if there's a single caret.
          return;
        }
        maybeCaret = carets.get(0);
      }
      if (maybeCaret == null) {
        return;
      }
      final Caret caret = maybeCaret;

      CodyCompletionElementRenderer completion = getCodyCompletionAtCaret(editor, caret);
      if (completion == null) {
        return;
      }
      WriteCommandAction.runWriteCommandAction(
          editor.getProject(),
          "Accept Cody Completion",
          "Cody", // TODO: what groupID should we use here?
          () -> {
            editor
                .getDocument()
                .replaceString(caret.getOffset(), caret.getOffset(), completion.text);
            editor.getCaretModel().moveToOffset(caret.getOffset() + completion.text.length());
            editor.getScrollingModel().scrollToCaret(ScrollType.MAKE_VISIBLE);
          });
    }
  }
}
