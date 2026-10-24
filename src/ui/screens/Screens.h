#pragma once

#include "../../core/Model.h"
#include "../../display/Display.h"

namespace mini_tv::screens {
void renderHome(Display& display, const AppState& state);
void renderHomeDynamic(Display& display, const AppState& state);
void renderMirror(Display& display, const AppState& state);
void renderMusic(Display& display, const AppState& state);
void renderMusicDynamic(Display& display, const AppState& state);
void renderDashboard(Display& display, const AppState& state);
void renderDashboardDynamic(Display& display, const AppState& state);
void renderSettings(Display& display, const AppState& state);
void renderError(Display& display, const AppState& state);
}  // namespace mini_tv::screens
