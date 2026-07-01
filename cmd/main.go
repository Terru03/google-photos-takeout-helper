/*
Google Photos Takeout Helper - A tool to clean and organize Google Photos Takeout exports
Copyright (C) 2026 feloex

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package main

import (
	"os"

	"github.com/Terru03/google-photos-takeout-helper/internal/cli"
	"github.com/Terru03/google-photos-takeout-helper/internal/gui"
)

func main() {
	// If args are provided, run cli
	if len(os.Args) > 1 {
		cli.Main()
		return
	}

	gui.Main()
}
