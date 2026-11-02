package tv.roxy.tv.ui.tv

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.focusable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.focus.FocusDirection
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.tv.material3.Text
import tv.roxy.tv.ui.theme.RoxyColors

/**
 * A text field for the D-pad. The field itself is not in the focus order: a wrapper takes
 * focus (accent ring), so Up / Down move on without the TV keyboard popping up. OK hands
 * focus to the field, which opens the keyboard; Back closes it, the next Up / Down leaves.
 */
@Composable
fun TvTextField(
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
    modifier: Modifier = Modifier,
    icon: ImageVector? = null,
    keyboardType: KeyboardType = KeyboardType.Text,
    imeAction: ImeAction = ImeAction.Next,
    password: Boolean = false,
    height: Dp = 50.dp,
    shape: Shape = androidx.compose.foundation.shape.RoundedCornerShape(12.dp),
    background: Color = RoxyColors.surfaceHigh,
    textColor: Color = RoxyColors.onBackground,
    placeholderColor: Color = RoxyColors.muted,
    fontSize: androidx.compose.ui.unit.TextUnit = 16.sp,
    onDone: () -> Unit = {},
) {
    val fieldFocus = remember { FocusRequester() }
    val wrapperFocus = remember { FocusRequester() }
    val focusManager = LocalFocusManager.current
    val keyboard = LocalSoftwareKeyboardController.current
    val interaction = remember { MutableInteractionSource() }
    val wrapperFocused by interaction.collectIsFocusedAsState()
    var editing by remember { mutableStateOf(false) }
    // Typing starts on an OK press that began here. The release of the press that opened the
    // page (OK on a profile, on the rail) used to land on a field focused at once and pop the keyboard.
    var okDownHere by remember { mutableStateOf(false) }
    LaunchedEffect(editing) { if (editing) { runCatching { fieldFocus.requestFocus() }; keyboard?.show() } }
    val ring = if (wrapperFocused || editing) RoxyColors.accent else Color.Transparent
    Row(
        modifier = modifier
            .fillMaxWidth()
            .height(height)
            .clip(shape)
            .background(background)
            .border(if (editing) 3.dp else 2.dp, ring, shape)
            .onKeyEvent { ev ->
                val ok = ev.key == Key.DirectionCenter || ev.key == Key.Enter || ev.key == Key.NumPadEnter
                when {
                    editing || !ok -> false
                    ev.type == KeyEventType.KeyDown -> { okDownHere = true; true }
                    ev.type == KeyEventType.KeyUp && okDownHere -> { okDownHere = false; editing = true; true }
                    else -> true // a release whose press happened elsewhere
                }
            }
            // Seen before the text field itself, which would otherwise swallow Up / Down as
            // cursor moves: while typing (keyboard already closed), Up / Down leave the
            // field and focus moves on from the wrapper; Back just stops typing.
            .onPreviewKeyEvent { ev ->
                when {
                    editing && ev.type == KeyEventType.KeyDown && (ev.key == Key.DirectionDown || ev.key == Key.DirectionUp) -> {
                        editing = false
                        keyboard?.hide()
                        runCatching { wrapperFocus.requestFocus() }
                        focusManager.moveFocus(if (ev.key == Key.DirectionDown) FocusDirection.Down else FocusDirection.Up)
                        true
                    }
                    editing && ev.type == KeyEventType.KeyDown && ev.key == Key.Back -> {
                        editing = false
                        keyboard?.hide()
                        runCatching { wrapperFocus.requestFocus() }
                        true
                    }
                    else -> false
                }
            }
            .focusRequester(wrapperFocus)
            .focusable(interactionSource = interaction)
            .padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (icon != null) Icon(icon, contentDescription = null, tint = if (wrapperFocused || editing) RoxyColors.accent else placeholderColor, modifier = Modifier.size(22.dp).padding(end = 0.dp))
        Box(modifier = Modifier.weight(1f).padding(start = if (icon != null) 12.dp else 0.dp), contentAlignment = Alignment.CenterStart) {
            if (value.isEmpty()) Text(placeholder, color = placeholderColor, fontSize = fontSize, maxLines = 1)
            BasicTextField(
                value = value, onValueChange = onValueChange, singleLine = true,
                textStyle = TextStyle(color = textColor, fontSize = fontSize),
                cursorBrush = SolidColor(RoxyColors.accent),
                visualTransformation = if (password) PasswordVisualTransformation() else VisualTransformation.None,
                keyboardOptions = KeyboardOptions(keyboardType = keyboardType, imeAction = imeAction, autoCorrectEnabled = false),
                // Every action key finishes typing: the Search key did nothing before.
                keyboardActions = KeyboardActions(
                    onDone = { editing = false; keyboard?.hide(); onDone() },
                    onSearch = { editing = false; keyboard?.hide(); onDone() },
                    onGo = { editing = false; keyboard?.hide(); onDone() },
                ),
                modifier = Modifier
                    .fillMaxWidth()
                    .focusRequester(fieldFocus)
                    .focusProperties { canFocus = editing }
                    // The field losing focus on its own (the keyboard going away on some
                    // TVs) hands focus back to the wrapper rather than to nothing.
                    .onFocusChanged { if (!it.isFocused && editing) { editing = false; runCatching { wrapperFocus.requestFocus() } } },
            )
        }
    }
}
