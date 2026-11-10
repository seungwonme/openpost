### Fixed

- Instagram carousels with video items now publish. Video items were sent without a media type, so Instagram rejected the first item.
- When Instagram rejects a carousel or Story container with a 4xx error, the destination now fails with that error instead of staying in "processing" forever. A carousel that was already stuck waiting after a lost or rejected container request creates the container again on the first check after the update, then continues publishing.
